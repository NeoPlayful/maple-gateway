package observer

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
	"github.com/NeoPlayful/maple-gateway/server/internal/containermanager/gwclient"
	"github.com/NeoPlayful/maple-gateway/server/internal/containermanager/nodes"
	"github.com/NeoPlayful/maple-gateway/server/internal/containermanager/tasksys"
	"go.uber.org/zap"
)

// fakeCommander 返回可编排的只读命令结果，替代真实 WS 任务通道。
type fakeCommander struct {
	mu      sync.Mutex
	results map[string]json.RawMessage
}

func newFakeCommander(containers string) *fakeCommander {
	return &fakeCommander{results: map[string]json.RawMessage{
		agentprotocol.ActionContainerList: json.RawMessage(containers),
		agentprotocol.ActionSystemInfo:    json.RawMessage(`{"id":"n","name":"n1","cpus":4,"memory_bytes":1024}`),
		agentprotocol.ActionNodeMetrics:   json.RawMessage(`{"host":{"available":false},"docker":{}}`),
	}}
}

func (f *fakeCommander) Set(action, result string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.results[action] = json.RawMessage(result)
}

func (f *fakeCommander) Call(_ context.Context, _, action string, _ any) (json.RawMessage, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if r, ok := f.results[action]; ok {
		return r, nil
	}
	return nil, nil
}

// Probe 同 Call：观测循环经此通道下达只读探测。
func (f *fakeCommander) Probe(_ context.Context, _, action string, _ any) (json.RawMessage, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if r, ok := f.results[action]; ok {
		return r, nil
	}
	return nil, nil
}

// SenderFor 实现 agentregistry.Commander：测试中不投递单向消息。
func (f *fakeCommander) SenderFor(string) (tasksys.Sender, bool) { return nil, false }

// failingCommander 令 container.list 始终失败，用于验证观测退避。
type failingCommander struct {
	mu    sync.Mutex
	calls int
}

func (f *failingCommander) Call(_ context.Context, _, action string, _ any) (json.RawMessage, error) {
	return f.fail(action)
}

func (f *failingCommander) Probe(_ context.Context, _, action string, _ any) (json.RawMessage, error) {
	return f.fail(action)
}

func (f *failingCommander) fail(action string) (json.RawMessage, error) {
	if action == agentprotocol.ActionContainerList {
		f.mu.Lock()
		f.calls++
		f.mu.Unlock()
		return nil, context.DeadlineExceeded
	}
	return json.RawMessage(`{}`), nil
}

func (f *failingCommander) SenderFor(string) (tasksys.Sender, bool) { return nil, false }

// 连续观测失败的节点应进入退避窗口，后续轮次跳过探测（不再打到节点）。
func TestObserverBacksOffOnRepeatedFailure(t *testing.T) {
	cmd := &failingCommander{}
	reg := onlineRegistry(cmd)
	obs := New(reg, gwclient.NewGatewayClient("", ""), time.Second, zap.NewNop())

	obs.tick(context.Background()) // 第一轮：探测失败，进入退避
	obs.tick(context.Background()) // 第二轮：处于退避窗口内，跳过探测
	obs.tick(context.Background())

	cmd.mu.Lock()
	calls := cmd.calls
	cmd.mu.Unlock()
	if calls != 1 {
		t.Errorf("container.list calls = %d, want 1 (subsequent ticks skipped by backoff)", calls)
	}
	if s := obs.Stats(); s.Degraded != 1 {
		t.Errorf("stats.Degraded = %d, want 1", s.Degraded)
	}
}

// onlineRegistry 构造一个含在线节点 node-uuid-1（名 node-01）的视图。
func onlineRegistry(cmd agentregistry.Commander) *agentregistry.Registry {
	st := nodes.New(nil, 0, 0)
	_, _ = st.Ensure(context.Background(), "node-uuid-1", "node-01", "", "", "")
	st.Touch("node-uuid-1")
	reg := agentregistry.New([]config.NodeConfig{{Name: "node-01", Host: "10.0.0.11"}}, cmd, st)
	reg.Bind("node-01", "node-uuid-1")
	return reg
}

// 记录 Gateway 收到的上报调用。
type gwRecorder struct {
	mu             sync.Mutex
	nodeReg        int
	nodeHB         int
	instReg        []map[string]any
	instHB         int
	instRegistered bool
}

func TestObserverReportsContainers(t *testing.T) {
	const iid = "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"

	containers := `[{"id":"c1","state":"running","instance_id":"` + iid + `",
		"host_port":8081,"labels":{"maple.managed":"true","maple.instance_id":"` + iid + `",
		"maple.service_id":"11111111-1111-1111-1111-111111111111"}}]`
	cmd := newFakeCommander(containers)
	reg := onlineRegistry(cmd)

	rec := &gwRecorder{}
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.mu.Lock()
		defer rec.mu.Unlock()
		switch {
		case r.URL.Path == "/api/internal/nodes/register":
			rec.nodeReg++
			_, _ = w.Write([]byte(`{"code":"OK","data":{"id":"node-uuid-1"}}`))
		case r.URL.Path == "/api/internal/nodes/node-uuid-1/heartbeat":
			rec.nodeHB++
			_, _ = w.Write([]byte(`{"code":"OK"}`))
		case r.URL.Path == "/api/internal/instances/register":
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			rec.instReg = append(rec.instReg, body)
			rec.instRegistered = true
			_, _ = w.Write([]byte(`{"code":"OK","data":{"id":"` + iid + `"}}`))
		case r.URL.Path == "/api/internal/instances/"+iid+"/heartbeat":
			if !rec.instRegistered {
				w.WriteHeader(http.StatusNotFound)
				_, _ = w.Write([]byte(`{"code":"NOT_FOUND"}`))
				return
			}
			rec.instHB++
			_, _ = w.Write([]byte(`{"code":"OK"}`))
		default:
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"code":"NOT_FOUND"}`))
		}
	}))
	defer gateway.Close()

	gw := gwclient.NewGatewayClient(gateway.URL, "internal-tok")
	obs := New(reg, gw, time.Second, zap.NewNop())

	obs.tick(context.Background())

	rec.mu.Lock()
	defer rec.mu.Unlock()
	// 节点身份在 Agent 接入时经 Gateway 解析并绑定，观测循环只发心跳、不再注册。
	if rec.nodeHB != 1 {
		t.Errorf("node heartbeat = %d, want 1", rec.nodeHB)
	}
	if len(rec.instReg) != 1 {
		t.Fatalf("instance register = %d, want 1", len(rec.instReg))
	}
	if rec.instReg[0]["id"] != iid {
		t.Errorf("registered instance id = %v, want %s", rec.instReg[0]["id"], iid)
	}
	if rec.instReg[0]["port"].(float64) != 8081 {
		t.Errorf("registered port = %v, want 8081", rec.instReg[0]["port"])
	}
	s := obs.Stats()
	if s.NodeUp != 1 || s.Containers != 1 {
		t.Errorf("stats = %+v, want node_up=1 containers=1", s)
	}
}

// 端口回填：容器创建时端口映射尚未就绪（host_port=0）即被注册，就绪后经心跳
// 带上真实宿主端口，Gateway 借此把实例端点从 0（退化为默认端口）补正为宿主端口。
func TestObserverHeartbeatCarriesHostPort(t *testing.T) {
	const iid = "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"

	// 首轮：容器已就绪但映射尚未填充 → host_port=0。
	cmd := newFakeCommander(`[{"id":"c1","state":"running","instance_id":"` + iid + `","host_port":0,
		"labels":{"maple.service_id":"11111111-1111-1111-1111-111111111111"}}]`)
	reg := onlineRegistry(cmd)

	var mu sync.Mutex
	var hbPorts []int
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		switch {
		case r.URL.Path == "/api/internal/nodes/register":
			_, _ = w.Write([]byte(`{"code":"OK","data":{"id":"node-uuid-1"}}`))
		case r.URL.Path == "/api/internal/instances/register":
			_, _ = w.Write([]byte(`{"code":"OK","data":{"id":"` + iid + `"}}`))
		case r.URL.Path == "/api/internal/instances/"+iid+"/heartbeat":
			var body struct {
				Port int `json:"port"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			hbPorts = append(hbPorts, body.Port)
			_, _ = w.Write([]byte(`{"code":"OK"}`))
		default:
			_, _ = w.Write([]byte(`{"code":"OK"}`))
		}
	}))
	defer gateway.Close()

	gw := gwclient.NewGatewayClient(gateway.URL, "internal-tok")
	obs := New(reg, gw, time.Second, zap.NewNop())

	obs.tick(context.Background()) // 首轮：host_port=0，注册
	cmd.Set(agentprotocol.ActionContainerList, `[{"id":"c1","state":"running","instance_id":"`+iid+
		`","host_port":64563,"labels":{"maple.service_id":"11111111-1111-1111-1111-111111111111"}}]`)
	obs.tick(context.Background()) // 次轮：映射就绪，心跳应带上 64563

	mu.Lock()
	defer mu.Unlock()
	if len(hbPorts) == 0 {
		t.Fatal("no instance heartbeat sent")
	}
	if got := hbPorts[len(hbPorts)-1]; got != 64563 {
		t.Errorf("last heartbeat port = %d, want 64563 (host port must be reported once ready)", got)
	}
}

// 容器消失：上一轮见过、本轮不在（节点仍可观测）→ 通知 Gateway 注销实例。
func TestObserverDeregistersVanished(t *testing.T) {
	const iid = "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"

	cmd := newFakeCommander(`[{"id":"c1","state":"running","instance_id":"` + iid + `","host_port":8081,
		"labels":{"maple.version_id":"v1","maple.service_id":"s1"}}]`)
	reg := onlineRegistry(cmd)

	var mu sync.Mutex
	deleted := 0
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		switch {
		case r.URL.Path == "/api/internal/nodes/register":
			_, _ = w.Write([]byte(`{"code":"OK","data":{"id":"node-uuid-1"}}`))
		case r.Method == http.MethodDelete && r.URL.Path == "/api/internal/instances/"+iid:
			deleted++
			_, _ = w.Write([]byte(`{"code":"OK"}`))
		default:
			_, _ = w.Write([]byte(`{"code":"OK"}`))
		}
	}))
	defer gateway.Close()

	gw := gwclient.NewGatewayClient(gateway.URL, "tok")
	obs := New(reg, gw, time.Second, zap.NewNop())

	obs.tick(context.Background()) // 第一轮：容器存在
	cmd.Set(agentprotocol.ActionContainerList, `[]`)
	obs.tick(context.Background()) // 第二轮：容器消失

	mu.Lock()
	defer mu.Unlock()
	if deleted != 1 {
		t.Errorf("deleted = %d, want 1", deleted)
	}
}

// 崩溃容器：非 running 的受管容器应上报为 unhealthy（可见），而非从上报流中消失。
func TestObserverReportsCrashedAsUnhealthy(t *testing.T) {
	const iid = "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"

	cmd := newFakeCommander(`[{"id":"c1","state":"exited","instance_id":"` + iid + `",
		"exit_code":137,"oom_killed":true,
		"labels":{"maple.service_id":"11111111-1111-1111-1111-111111111111"}}]`)
	reg := onlineRegistry(cmd)

	var mu sync.Mutex
	healthReports := []string{}
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		switch {
		case r.URL.Path == "/api/internal/nodes/register":
			_, _ = w.Write([]byte(`{"code":"OK","data":{"id":"node-uuid-1"}}`))
		case r.URL.Path == "/api/internal/instances/"+iid+"/health":
			var body struct {
				Health string `json:"health"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			healthReports = append(healthReports, body.Health)
			_, _ = w.Write([]byte(`{"code":"OK"}`))
		default:
			_, _ = w.Write([]byte(`{"code":"OK"}`))
		}
	}))
	defer gateway.Close()

	gw := gwclient.NewGatewayClient(gateway.URL, "tok")
	obs := New(reg, gw, time.Second, zap.NewNop())

	obs.tick(context.Background())

	mu.Lock()
	defer mu.Unlock()
	found := false
	for _, h := range healthReports {
		if h == "unhealthy" {
			found = true
		}
	}
	if !found {
		t.Errorf("health reports = %v, want an unhealthy report for crashed instance", healthReports)
	}
	var hasErr bool
	for _, e := range obs.RuntimeErrors() {
		if e.InstanceID == iid && e.ExitCode == 137 && e.OOMKilled {
			hasErr = true
		}
	}
	if !hasErr {
		t.Errorf("runtime errors = %+v, want an entry for %s (exit 137, oom)", obs.RuntimeErrors(), iid)
	}
}
