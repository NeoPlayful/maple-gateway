package reconciler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/NeoPlayful/maple-gateway/server/internal/agentprotocol"
	"github.com/NeoPlayful/maple-gateway/server/internal/containermanager/agentregistry"
	"github.com/NeoPlayful/maple-gateway/server/internal/containermanager/config"
	"github.com/NeoPlayful/maple-gateway/server/internal/containermanager/desired"
	"github.com/NeoPlayful/maple-gateway/server/internal/containermanager/gwclient"
	"github.com/NeoPlayful/maple-gateway/server/internal/containermanager/nodes"
	"github.com/NeoPlayful/maple-gateway/server/internal/containermanager/observer"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

// fakeCommander 记录下发的 action 并返回可编排的结果，替代真实 WS 任务通道。
type fakeCommander struct {
	mu      sync.Mutex
	calls   []string
	results map[string]json.RawMessage
}

func newFakeCommander() *fakeCommander {
	return &fakeCommander{results: map[string]json.RawMessage{
		agentprotocol.ActionContainerCreate: json.RawMessage(`{"container_id":"c","host_port":9001}`),
	}}
}

func (f *fakeCommander) Call(_ context.Context, _, action string, _ any) (json.RawMessage, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, action)
	if r, ok := f.results[action]; ok {
		return r, nil
	}
	return nil, nil
}

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

// newOnlineRegistry 构造含一个在线节点的视图（命令走 fakeCommander）。
func newOnlineRegistry(cmd agentregistry.Commander) *agentregistry.Registry {
	st := nodes.New(nil, 0, 0)
	st.Ensure(context.Background(), "gw-node-1", "n1", "", "", "")
	st.Touch("gw-node-1")
	reg := agentregistry.New([]config.NodeConfig{{Name: "n1", Host: "10.0.0.1"}}, cmd, st)
	reg.Bind("n1", "gw-node-1")
	return reg
}

// fakeDesired 是可控的期望态读取器。
type fakeDesired struct {
	states []desired.State
	paused map[string]int
	phases map[uuid.UUID]desired.Phase
}

func (f *fakeDesired) All() []desired.State { return f.states }

func (f *fakeDesired) PausedCount() map[string]int {
	if f.paused == nil {
		return map[string]int{}
	}
	return f.paused
}

func (f *fakeDesired) SetPhase(deploymentID uuid.UUID, phase desired.Phase) {
	if f.phases == nil {
		f.phases = map[uuid.UUID]desired.Phase{}
	}
	f.phases[deploymentID] = phase
}

// fakeActual 是可控的实际态读取器。
type fakeActual struct {
	snap map[string]observer.ObservedContainer
}

func (f *fakeActual) Snapshot() map[string]observer.ObservedContainer { return f.snap }

func TestReconcileScaleUp(t *testing.T) {
	cmd := newFakeCommander()
	reg := newOnlineRegistry(cmd)

	verID := uuid.New()
	fd := &fakeDesired{states: []desired.State{{
		DeploymentID: uuid.New(), ServiceID: uuid.New(), VersionID: verID,
		Image: "nginx:alpine", Port: 80, Replicas: 3,
	}}}
	fa := &fakeActual{snap: map[string]observer.ObservedContainer{}}

	gw := gwclient.NewGatewayClient("", "")
	r := New(fd, fa, reg, gw, time.Second, zap.NewNop())
	r.Reconcile(context.Background())

	if got := cmd.count(agentprotocol.ActionContainerCreate); got != 3 {
		t.Errorf("created = %d, want 3", got)
	}
}

// 观测滞后场景：连续多轮 reconcile 之间实际态快照尚未反映首轮创建的容器，
// in-flight 计数应阻止重复创建（否则会超配到 3）。
func TestReconcileNoOverProvisionOnObserveLag(t *testing.T) {
	cmd := newFakeCommander()
	reg := newOnlineRegistry(cmd)

	fd := &fakeDesired{states: []desired.State{{
		DeploymentID: uuid.New(), ServiceID: uuid.New(), VersionID: uuid.New(),
		Image: "nginx:alpine", Port: 80, Replicas: 2,
	}}}
	fa := &fakeActual{snap: map[string]observer.ObservedContainer{}}
	gw := gwclient.NewGatewayClient("", "")
	r := New(fd, fa, reg, gw, time.Second, zap.NewNop())

	r.Reconcile(context.Background())
	r.Reconcile(context.Background())
	r.Reconcile(context.Background())

	if got := cmd.count(agentprotocol.ActionContainerCreate); got != 2 {
		t.Errorf("created = %d, want 2 (in-flight should suppress over-provisioning)", got)
	}
}

// 人工维护场景：管理员手动停掉的实例被登记为 paused，对账器应计入实际数、不再补回。
func TestReconcileRespectsOperatorPaused(t *testing.T) {
	cmd := newFakeCommander()
	reg := newOnlineRegistry(cmd)

	verID := uuid.New()
	fd := &fakeDesired{
		states: []desired.State{{
			DeploymentID: uuid.New(), ServiceID: uuid.New(), VersionID: verID,
			Image: "nginx:alpine", Port: 80, Replicas: 2,
		}},
		paused: map[string]int{verID.String(): 1},
	}
	fa := &fakeActual{snap: map[string]observer.ObservedContainer{}}
	gw := gwclient.NewGatewayClient("", "")
	r := New(fd, fa, reg, gw, time.Second, zap.NewNop())
	r.Reconcile(context.Background())

	if got := cmd.count(agentprotocol.ActionContainerCreate); got != 1 {
		t.Errorf("created = %d, want 1 (paused instance counts toward replicas)", got)
	}
}

func TestReconcileScaleDownDrainsFirst(t *testing.T) {
	cmd := newFakeCommander()

	drained := false
	var mu sync.Mutex
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if r.URL.Path == "/api/internal/instances/i1/drain" {
			drained = true
		}
		_, _ = w.Write([]byte(`{"code":"OK"}`))
	}))
	defer gateway.Close()

	reg := newOnlineRegistry(cmd)

	verID := uuid.New()
	fd := &fakeDesired{states: []desired.State{{VersionID: verID, Replicas: 0, Image: "x"}}}
	fa := &fakeActual{snap: map[string]observer.ObservedContainer{
		"i1": {
			NodeName: "n1", InstanceID: "i1",
			Container: gwclient.Container{
				ID: "c1", State: "running", InstanceID: "i1",
				Labels: map[string]string{"maple.version_id": verID.String()},
			},
		},
	}}
	gw := gwclient.NewGatewayClient(gateway.URL, "tok")
	r := New(fd, fa, reg, gw, time.Second, zap.NewNop())
	r.Reconcile(context.Background())

	mu.Lock()
	defer mu.Unlock()
	if !drained {
		t.Error("expected drain before stop")
	}
	if cmd.count(agentprotocol.ActionContainerStop) != 1 || cmd.count(agentprotocol.ActionContainerRemove) != 1 {
		t.Errorf("stopped=%d removed=%d, want 1/1",
			cmd.count(agentprotocol.ActionContainerStop), cmd.count(agentprotocol.ActionContainerRemove))
	}
}
