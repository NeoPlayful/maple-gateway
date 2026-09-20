package control

import (
	"context"
	"encoding/json"
	"sync"
	"testing"

	"github.com/NeoPlayful/maple-gateway/server/internal/agentprotocol"
	"github.com/NeoPlayful/maple-gateway/server/internal/containermanager/agentregistry"
	"github.com/NeoPlayful/maple-gateway/server/internal/containermanager/config"
	"github.com/NeoPlayful/maple-gateway/server/internal/containermanager/desired"
	"github.com/NeoPlayful/maple-gateway/server/internal/containermanager/gwclient"
	"github.com/NeoPlayful/maple-gateway/server/internal/containermanager/nodes"
	"github.com/NeoPlayful/maple-gateway/server/internal/containermanager/observer"
	"github.com/NeoPlayful/maple-gateway/server/internal/containermanager/tasksys"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

// fakeCommander 记录下发的 action，替代真实 WS 任务通道。
type fakeCommander struct {
	mu    sync.Mutex
	calls []string
}

func (f *fakeCommander) Call(_ context.Context, _, action string, _ any) (json.RawMessage, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, action)
	return nil, nil
}

func (f *fakeCommander) Probe(ctx context.Context, _, action string, _ any) (json.RawMessage, error) {
	return f.Call(ctx, "", action, nil)
}

func (f *fakeCommander) SenderFor(string) (tasksys.Sender, bool) { return nil, false }

func (f *fakeCommander) count(action string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, a := range f.calls {
		if a == action {
			n++
		}
	}
	return n
}

// fakeActual 提供可控的容器快照。
type fakeActual struct {
	snap map[string]observer.ObservedContainer
}

func (f *fakeActual) Snapshot() map[string]observer.ObservedContainer { return f.snap }
func (f *fakeActual) Containers() map[string]observer.ObservedContainer {
	return f.snap
}

func newOnlineRegistry(cmd agentregistry.Commander) *agentregistry.Registry {
	st := nodes.New(nil, 0, 0)
	st.Ensure(context.Background(), "gw-node-1", "n1", "", "", "")
	st.Touch("gw-node-1")
	reg := agentregistry.New([]config.NodeConfig{{Name: "n1", Host: "10.0.0.1"}}, cmd, st)
	reg.Bind("n1", "gw-node-1")
	return reg
}

// obsContainer 构造一个带部署标签的受管容器观测项。
func obsContainer(instanceID, deploymentID string) observer.ObservedContainer {
	return observer.ObservedContainer{
		NodeName: "n1", InstanceID: instanceID,
		Container: gwclient.Container{
			ID: "c-" + instanceID, State: "running", InstanceID: instanceID,
			Labels: map[string]string{"maple.deployment_id": deploymentID},
		},
	}
}

// 停止/删除部署时，应回收该部署名下全部容器，且不触碰其他部署的容器。
func TestRemoveDeploymentRemovesOnlyItsContainers(t *testing.T) {
	cmd := &fakeCommander{}
	reg := newOnlineRegistry(cmd)

	depA := uuid.NewString()
	depB := uuid.NewString()
	fa := &fakeActual{snap: map[string]observer.ObservedContainer{
		"iA1": obsContainer("iA1", depA),
		"iA2": obsContainer("iA2", depA),
		"iB1": obsContainer("iB1", depB),
	}}
	c := New(reg, fa, desired.NewStore(nil), nil, zap.NewNop())

	got := c.RemoveDeployment(context.Background(), depA)
	if got != 2 {
		t.Errorf("removed = %d, want 2", got)
	}
	if n := cmd.count(agentprotocol.ActionContainerRemove); n != 2 {
		t.Errorf("remove calls = %d, want 2 (only deployment A)", n)
	}
}

// obsContainerVer 构造一个带部署+版本标签的受管容器观测项。
func obsContainerVer(instanceID, deploymentID, versionID string) observer.ObservedContainer {
	return observer.ObservedContainer{
		NodeName: "n1", InstanceID: instanceID,
		Container: gwclient.Container{
			ID: "c-" + instanceID, State: "running", InstanceID: instanceID,
			Labels: map[string]string{
				"maple.deployment_id": deploymentID,
				"maple.version_id":    versionID,
			},
		},
	}
}

// 删除版本时只应回收该版本的容器，同部署其它版本的容器必须原封不动。
func TestRemoveVersionRemovesOnlyItsVersion(t *testing.T) {
	cmd := &fakeCommander{}
	reg := newOnlineRegistry(cmd)

	dep := uuid.NewString()
	v3 := uuid.NewString()
	v2 := uuid.NewString()
	fa := &fakeActual{snap: map[string]observer.ObservedContainer{
		"i31": obsContainerVer("i31", dep, v3),
		"i32": obsContainerVer("i32", dep, v3),
		"i21": obsContainerVer("i21", dep, v2),
	}}
	c := New(reg, fa, desired.NewStore(nil), nil, zap.NewNop())

	got := c.RemoveVersion(context.Background(), dep, v3)
	if got != 2 {
		t.Errorf("removed = %d, want 2 (only version v3)", got)
	}
	if n := cmd.count(agentprotocol.ActionContainerRemove); n != 2 {
		t.Errorf("remove calls = %d, want 2 (must not touch v2)", n)
	}
}

// 空 version_id 不应发起任何删除。
func TestRemoveVersionEmptyIDIsNoop(t *testing.T) {
	cmd := &fakeCommander{}
	reg := newOnlineRegistry(cmd)
	fa := &fakeActual{snap: map[string]observer.ObservedContainer{
		"i1": obsContainerVer("i1", uuid.NewString(), uuid.NewString()),
	}}
	c := New(reg, fa, desired.NewStore(nil), nil, zap.NewNop())

	if got := c.RemoveVersion(context.Background(), "", ""); got != 0 {
		t.Errorf("removed = %d, want 0", got)
	}
	if n := cmd.count(agentprotocol.ActionContainerRemove); n != 0 {
		t.Errorf("remove calls = %d, want 0", n)
	}
}

// 空 deployment_id 不应发起任何删除。
func TestRemoveDeploymentEmptyIDIsNoop(t *testing.T) {
	cmd := &fakeCommander{}
	reg := newOnlineRegistry(cmd)
	c := New(reg, &fakeActual{snap: map[string]observer.ObservedContainer{}}, desired.NewStore(nil), nil, zap.NewNop())

	if got := c.RemoveDeployment(context.Background(), ""); got != 0 {
		t.Errorf("removed = %d, want 0", got)
	}
	if n := cmd.count(agentprotocol.ActionContainerRemove); n != 0 {
		t.Errorf("remove calls = %d, want 0", n)
	}
}

// idRecordingCommander 记录每次下发动作携带的 ID 入参。
type idRecordingCommander struct {
	mu   sync.Mutex
	ids  []string
	acts []string
}

func (f *idRecordingCommander) Call(_ context.Context, _, action string, params any) (json.RawMessage, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.acts = append(f.acts, action)
	switch p := params.(type) {
	case agentprotocol.IDParams:
		f.ids = append(f.ids, p.ID)
	case agentprotocol.RemoveParams:
		f.ids = append(f.ids, p.ID)
	case agentprotocol.LogsReadParams:
		f.ids = append(f.ids, p.ID)
	}
	return nil, nil
}

func (f *idRecordingCommander) Probe(ctx context.Context, _, action string, params any) (json.RawMessage, error) {
	return f.Call(ctx, "", action, params)
}

func (f *idRecordingCommander) SenderFor(string) (tasksys.Sender, bool) { return nil, false }

// 逐容器操作应下发真实容器 ID，而非（Compose 容器派生出的）instance_id：
// 派生 ID 并非容器标签，下发它会在 Agent 侧解析失败；容器 ID 恒可直达。
func TestControlSendsRealContainerID(t *testing.T) {
	const derivedID = "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
	const realCID = "sha256realcontainerid"

	cmd := &idRecordingCommander{}
	reg := newOnlineRegistry(cmd)
	fa := &fakeActual{snap: map[string]observer.ObservedContainer{
		derivedID: {
			NodeName: "n1", InstanceID: derivedID,
			Container: gwclient.Container{
				ID: realCID, Name: "maple-574aa2d4d469-web-1", State: "running", InstanceID: derivedID,
			},
		},
	}}
	c := New(reg, fa, desired.NewStore(nil), nil, zap.NewNop())

	if err := c.Stop(context.Background(), derivedID); err != nil {
		t.Fatalf("stop: %v", err)
	}
	if err := c.Restart(context.Background(), derivedID); err != nil {
		t.Fatalf("restart: %v", err)
	}
	if err := c.Remove(context.Background(), derivedID, true); err != nil {
		t.Fatalf("remove: %v", err)
	}

	cmd.mu.Lock()
	defer cmd.mu.Unlock()
	if len(cmd.ids) != 3 {
		t.Fatalf("recorded ids = %v, want 3", cmd.ids)
	}
	for i, id := range cmd.ids {
		if id != realCID {
			t.Errorf("action %s sent id = %q, want real container id %q", cmd.acts[i], id, realCID)
		}
	}
}
