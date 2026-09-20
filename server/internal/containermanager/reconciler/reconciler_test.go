package reconciler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
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
	"github.com/NeoPlayful/maple-gateway/server/internal/containermanager/tasksys"
	"github.com/NeoPlayful/maple-gateway/server/pkg"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

// fakeCommander 记录下发的 action 并返回可编排的结果，替代真实 WS 任务通道。
type fakeCommander struct {
	mu      sync.Mutex
	calls   []string
	results map[string]json.RawMessage
	fail    map[string]bool // 该 action 是否固定失败
	// createSpecs 记录每次 container.create 的规格，供断言容器名与序号。
	createSpecs []gwclient.CreateSpec
}

func newFakeCommander() *fakeCommander {
	return &fakeCommander{results: map[string]json.RawMessage{
		agentprotocol.ActionContainerCreate: json.RawMessage(`{"container_id":"c","host_port":9001}`),
	}}
}

func (f *fakeCommander) Call(_ context.Context, _, action string, params any) (json.RawMessage, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, action)
	if f.fail[action] {
		return nil, errors.New("boom")
	}
	if action == agentprotocol.ActionContainerCreate {
		if spec, ok := params.(gwclient.CreateSpec); ok {
			f.createSpecs = append(f.createSpecs, spec)
		}
	}
	if r, ok := f.results[action]; ok {
		return r, nil
	}
	return nil, nil
}

// names 返回本次记录的全部容器名（按创建顺序）。
func (f *fakeCommander) names() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, 0, len(f.createSpecs))
	for _, s := range f.createSpecs {
		out = append(out, s.Name)
	}
	return out
}

// Probe 同为请求/响应通道；对账测试不区分记录与否，行为与 Call 一致。
func (f *fakeCommander) Probe(ctx context.Context, _, action string, _ any) (json.RawMessage, error) {
	return f.Call(ctx, "", action, nil)
}

// SenderFor 实现 agentregistry.Commander：测试中不投递单向消息。
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
	paused map[string]string // instance_id → version_id
	phases map[uuid.UUID]desired.Phase
}

func (f *fakeDesired) All() []desired.State { return f.states }

func (f *fakeDesired) PausedInstances() map[string]string {
	if f.paused == nil {
		return map[string]string{}
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
		paused: map[string]string{"i-paused": verID.String()},
	}
	fa := &fakeActual{snap: map[string]observer.ObservedContainer{}}
	gw := gwclient.NewGatewayClient("", "")
	r := New(fd, fa, reg, gw, time.Second, zap.NewNop())
	r.Reconcile(context.Background())

	if got := cmd.count(agentprotocol.ActionContainerCreate); got != 1 {
		t.Errorf("created = %d, want 1 (paused instance counts toward replicas)", got)
	}
}

// 停止滞后误删场景（回归）：管理员手动 stop 后，观测快照尚未刷新为 exited，
// 该实例仍以 running 出现；此时它既在 paused 集合里、又在 running 计数里。
// 正确行为：该实例只计一份 → 实际数 == 目标数 → 既不下发 stop 也不下发 remove。
// 修复前 running + paused 被算成两份，对账器判定超编并把管理员刚停的实例删掉。
func TestReconcileDoesNotDrainPausedInstanceOnObserveLag(t *testing.T) {
	cmd := newFakeCommander()
	reg := newOnlineRegistry(cmd)

	verID := uuid.New()
	depID := uuid.New()
	pausedID := uuid.NewString()
	fd := &fakeDesired{
		states: []desired.State{{
			DeploymentID: depID, ServiceID: uuid.New(), VersionID: verID,
			Image: "nginx:alpine", Port: 80, Replicas: 1,
		}},
		paused: map[string]string{pausedID: verID.String()},
	}
	// 快照里该实例仍是 running（停止尚未被观测到）。
	fa := &fakeActual{snap: map[string]observer.ObservedContainer{
		pausedID: {
			NodeName: "n1", InstanceID: pausedID,
			Container: gwclient.Container{
				ID: "c1", State: "running", InstanceID: pausedID,
				Labels: map[string]string{"maple.version_id": verID.String()},
			},
		},
	}}
	gw := gwclient.NewGatewayClient("", "")
	r := New(fd, fa, reg, gw, time.Second, zap.NewNop())
	r.Reconcile(context.Background())

	if got := cmd.count(agentprotocol.ActionContainerStop); got != 0 {
		t.Errorf("stop = %d, want 0 (paused instance must not be stopped)", got)
	}
	if got := cmd.count(agentprotocol.ActionContainerRemove); got != 0 {
		t.Errorf("remove = %d, want 0 (paused instance must not be drained)", got)
	}
	if got := cmd.count(agentprotocol.ActionContainerCreate); got != 0 {
		t.Errorf("create = %d, want 0 (target already satisfied by paused instance)", got)
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

// 创建持续失败场景：失败计数按部署聚合，达 maxFailures 后停止补副本，
// 避免每次新 instance_id 导致计数不累加、无限重建。
func TestReconcileStopsAfterRepeatedCreateFailure(t *testing.T) {
	cmd := newFakeCommander()
	cmd.fail = map[string]bool{agentprotocol.ActionContainerCreate: true}
	reg := newOnlineRegistry(cmd)

	verID := uuid.New()
	depID := uuid.New()
	fd := &fakeDesired{states: []desired.State{{
		DeploymentID: depID, ServiceID: uuid.New(), VersionID: verID,
		Image: "x", Port: 80, Replicas: 1,
	}}}
	fa := &fakeActual{snap: map[string]observer.ObservedContainer{}}
	gw := gwclient.NewGatewayClient("", "")
	r := New(fd, fa, reg, gw, time.Second, zap.NewNop())

	for i := 0; i < maxFailures+5; i++ {
		r.Reconcile(context.Background())
	}

	if got := cmd.count(agentprotocol.ActionContainerCreate); got != maxFailures {
		t.Errorf("created = %d, want %d (bounded retry)", got, maxFailures)
	}
	if fd.phases[depID].Status != "failed" {
		t.Errorf("phase = %q, want failed", fd.phases[depID].Status)
	}
}

// 容器命名：有项目归属时按 Compose 风格 maple-<项目短码>-<版本>-<序号> 取名，
// 且同一版本内多副本序号互不相同（从 1 起递增）。
func TestReconcileNamesContainersWithProject(t *testing.T) {
	cmd := newFakeCommander()
	reg := newOnlineRegistry(cmd)

	projID := uuid.New()
	verID := uuid.New()
	fd := &fakeDesired{states: []desired.State{{
		DeploymentID: uuid.New(), ServiceID: uuid.New(), VersionID: verID,
		ProjectID: &projID, Version: "v3", Image: "nginx:alpine", Port: 80, Replicas: 3,
	}}}
	fa := &fakeActual{snap: map[string]observer.ObservedContainer{}}
	gw := gwclient.NewGatewayClient("", "")
	r := New(fd, fa, reg, gw, time.Second, zap.NewNop())
	r.Reconcile(context.Background())

	names := cmd.names()
	if len(names) != 3 {
		t.Fatalf("names = %v, want 3", names)
	}
	prefix := "maple-" + pkg.ShortID(projID.String()) + "-v3-"
	seen := map[string]bool{}
	for _, n := range names {
		if !strings.HasPrefix(n, prefix) {
			t.Errorf("name %q, want prefix %q", n, prefix)
		}
		if seen[n] {
			t.Errorf("duplicate name %q", n)
		}
		seen[n] = true
	}
	// 序号从 1 起，连续覆盖 1..3。
	for _, idx := range []int{1, 2, 3} {
		if !seen[prefix+strconv.Itoa(idx)] {
			t.Errorf("missing name %q in %v", prefix+strconv.Itoa(idx), names)
		}
	}
}

// 已用序号（观测到的存活容器）在新建时被跳过：新建副本应取下一个空位，不与存量撞名。
func TestReconcileSkipsUsedIndexes(t *testing.T) {
	cmd := newFakeCommander()
	reg := newOnlineRegistry(cmd)

	projID := uuid.New()
	verID := uuid.New()
	fd := &fakeDesired{states: []desired.State{{
		DeploymentID: uuid.New(), ServiceID: uuid.New(), VersionID: verID,
		ProjectID: &projID, Version: "v1", Image: "nginx:alpine", Port: 80, Replicas: 2,
	}}}
	// 存量副本已占序号 1，目标 2 副本 → 只需补 1 个，应取名 -2。
	fa := &fakeActual{snap: map[string]observer.ObservedContainer{
		"i1": {
			NodeName: "n1", InstanceID: "i1",
			Container: gwclient.Container{
				ID: "c1", State: "running", InstanceID: "i1",
				Labels: map[string]string{
					"maple.version_id": verID.String(),
					replicaIndexLabel:  "1",
				},
			},
		},
	}}
	gw := gwclient.NewGatewayClient("", "")
	r := New(fd, fa, reg, gw, time.Second, zap.NewNop())
	r.Reconcile(context.Background())

	names := cmd.names()
	if len(names) != 1 {
		t.Fatalf("names = %v, want 1 new container", names)
	}
	want := "maple-" + pkg.ShortID(projID.String()) + "-v1-2"
	if names[0] != want {
		t.Errorf("name = %q, want %q (must skip used index 1)", names[0], want)
	}
}

// 项目为空：不下发容器名（由 Agent 退回 maple-<实例短码>），序号标签仍分配。
func TestReconcileNoNameWithoutProject(t *testing.T) {
	cmd := newFakeCommander()
	reg := newOnlineRegistry(cmd)

	fd := &fakeDesired{states: []desired.State{{
		DeploymentID: uuid.New(), ServiceID: uuid.New(), VersionID: uuid.New(),
		Image: "nginx:alpine", Port: 80, Replicas: 1,
	}}}
	fa := &fakeActual{snap: map[string]observer.ObservedContainer{}}
	gw := gwclient.NewGatewayClient("", "")
	r := New(fd, fa, reg, gw, time.Second, zap.NewNop())
	r.Reconcile(context.Background())

	names := cmd.names()
	if len(names) != 1 {
		t.Fatalf("names = %v, want 1", names)
	}
	if names[0] != "" {
		t.Errorf("name = %q, want empty (Agent falls back to instance short id)", names[0])
	}
}

// 崩溃副本场景：在途副本被观测为 exited 时应被回收（强制删除），
// 而非留在原地或反复新建。
func TestReconcileRecyclesCrashedReplica(t *testing.T) {
	cmd := newFakeCommander()
	reg := newOnlineRegistry(cmd)

	verID := uuid.New()
	depID := uuid.New()
	fd := &fakeDesired{states: []desired.State{{
		DeploymentID: depID, ServiceID: uuid.New(), VersionID: verID,
		Image: "bad:latest", Port: 80, Replicas: 1,
	}}}
	iid := uuid.NewString()
	fa := &fakeActual{snap: map[string]observer.ObservedContainer{
		iid: {
			NodeName: "n1", InstanceID: iid,
			Container: gwclient.Container{
				ID: "c1", State: "exited", InstanceID: iid,
				Labels: map[string]string{"maple.version_id": verID.String()},
			},
		},
	}}
	gw := gwclient.NewGatewayClient("", "")
	r := New(fd, fa, reg, gw, time.Second, zap.NewNop())
	r.pending[iid] = pendingRpl{versionID: verID.String(), deploymentID: depID.String(), createdAt: time.Now()}

	r.Reconcile(context.Background())

	if got := cmd.count(agentprotocol.ActionContainerRemove); got != 1 {
		t.Errorf("recycled = %d, want 1", got)
	}
	// 崩溃副本被回收后补一个新副本（未达失败上限）。
	if got := cmd.count(agentprotocol.ActionContainerCreate); got != 1 {
		t.Errorf("created = %d, want 1", got)
	}
}
