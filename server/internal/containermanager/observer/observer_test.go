package observer

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/NeoPlayful/maple-gateway/server/internal/containermanager/agentregistry"
	"github.com/NeoPlayful/maple-gateway/server/internal/containermanager/config"
	"github.com/NeoPlayful/maple-gateway/server/internal/containermanager/gwclient"
	"go.uber.org/zap"
)

// 记录 Gateway 收到的上报调用。
type gwRecorder struct {
	mu             sync.Mutex
	nodeReg        int
	nodeHB         int
	instReg        []map[string]any
	instHB         int
	instRegistered bool // 实例已注册后心跳才返回 200（否则 404，触发注册）
}

func TestObserverReportsContainers(t *testing.T) {
	const iid = "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"

	// 假 Agent：返回一个 running 的受管容器。
	agent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/health":
			_, _ = w.Write([]byte(`{"status":"ok"}`))
		case "/api/internal/containers":
			_, _ = w.Write([]byte(`[{"id":"c1","state":"running","instance_id":"` + iid + `",
				"host_port":8081,"labels":{"maple.managed":"true","maple.instance_id":"` + iid + `",
				"maple.service_id":"11111111-1111-1111-1111-111111111111"}}]`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer agent.Close()

	// 假 Gateway：记录节点/实例上报。
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
			// 未知路径：Gateway 返回 404（与真实行为一致）。
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"code":"NOT_FOUND"}`))
		}
	}))
	defer gateway.Close()

	reg := agentregistry.New([]config.NodeConfig{{
		Name: "node-01", Host: "10.0.0.11", AgentAddr: agent.URL, AgentToken: "tok",
	}})
	gw := gwclient.NewGatewayClient(gateway.URL, "internal-tok")
	obs := New(reg, gw, time.Second, zap.NewNop())

	obs.tick(context.Background())

	rec.mu.Lock()
	defer rec.mu.Unlock()
	if rec.nodeReg != 1 {
		t.Errorf("node register = %d, want 1", rec.nodeReg)
	}
	// 首轮：实例心跳失败 → 注册；故注册数应为 1。
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

// 容器消失：上一轮见过、本轮不在（节点仍可观测）→ 通知 Gateway 注销实例。
func TestObserverDeregistersVanished(t *testing.T) {
	const iid = "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
	var present sync.Mutex
	has := true

	agent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		present.Lock()
		defer present.Unlock()
		switch r.URL.Path {
		case "/health":
			_, _ = w.Write([]byte(`{"status":"ok"}`))
		case "/api/internal/containers":
			if !has {
				_, _ = w.Write([]byte(`[]`))
				return
			}
			_, _ = w.Write([]byte(`[{"id":"c1","state":"running","instance_id":"` + iid + `","host_port":8081,
				"labels":{"maple.version_id":"v1","maple.service_id":"s1"}}]`))
		}
	}))
	defer agent.Close()

	var mu sync.Mutex
	deleted := 0
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		switch {
		case r.URL.Path == "/api/internal/nodes/register":
			_, _ = w.Write([]byte(`{"code":"OK","data":{"id":"n1"}}`))
		case r.Method == http.MethodDelete && r.URL.Path == "/api/internal/instances/"+iid:
			deleted++
			_, _ = w.Write([]byte(`{"code":"OK"}`))
		case r.URL.Path == "/api/internal/instances/"+iid+"/heartbeat":
			_, _ = w.Write([]byte(`{"code":"OK"}`))
		default:
			_, _ = w.Write([]byte(`{"code":"OK"}`))
		}
	}))
	defer gateway.Close()

	reg := agentregistry.New([]config.NodeConfig{{Name: "n1", Host: "10.0.0.1", AgentAddr: agent.URL}})
	gw := gwclient.NewGatewayClient(gateway.URL, "tok")
	obs := New(reg, gw, time.Second, zap.NewNop())

	obs.tick(context.Background()) // 第一轮：容器存在
	present.Lock()
	has = false
	present.Unlock()
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

	agent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/health":
			_, _ = w.Write([]byte(`{"status":"ok"}`))
		case "/api/internal/node/metrics":
			_, _ = w.Write([]byte(`{"host":{"available":false}}`))
		case "/api/internal/containers":
			// exited 容器：无 host_port、带退出码。
			_, _ = w.Write([]byte(`[{"id":"c1","state":"exited","instance_id":"` + iid + `",
				"exit_code":137,"oom_killed":true,
				"labels":{"maple.service_id":"11111111-1111-1111-1111-111111111111"}}]`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer agent.Close()

	var mu sync.Mutex
	healthReports := []string{}
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		switch {
		case r.URL.Path == "/api/internal/nodes/register":
			_, _ = w.Write([]byte(`{"code":"OK","data":{"id":"n1"}}`))
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

	reg := agentregistry.New([]config.NodeConfig{{Name: "n1", Host: "10.0.0.1", AgentAddr: agent.URL}})
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
	// 运行时报错应记录该实例。
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
