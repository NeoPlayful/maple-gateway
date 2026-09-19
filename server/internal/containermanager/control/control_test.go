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
