package reconciler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/NeoPlayful/maple-gateway/server/internal/containermanager/agentregistry"
	"github.com/NeoPlayful/maple-gateway/server/internal/containermanager/config"
	"github.com/NeoPlayful/maple-gateway/server/internal/containermanager/desired"
	"github.com/NeoPlayful/maple-gateway/server/internal/containermanager/gwclient"
	"github.com/NeoPlayful/maple-gateway/server/internal/containermanager/observer"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

// fakeDesired 是可控的期望态读取器。
type fakeDesired struct{ states []desired.State }

func (f *fakeDesired) All() []desired.State { return f.states }

// fakeActual 是可控的实际态读取器。
type fakeActual struct{ snap map[string]observer.ObservedContainer }

func (f *fakeActual) Snapshot() map[string]observer.ObservedContainer { return f.snap }

func TestReconcileScaleUp(t *testing.T) {
	var mu sync.Mutex
	created := 0
	agent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/api/internal/containers" {
			mu.Lock()
			created++
			mu.Unlock()
			_, _ = w.Write([]byte(`{"id":"c","instance_id":"x","host_port":9001}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer agent.Close()

	reg := agentregistry.New([]config.NodeConfig{{Name: "n1", Host: "10.0.0.1", AgentAddr: agent.URL}})
	// 标记健康（模拟已探测）。
	reg.SetHealth("n1", true, "gw-node-1", time.Now().UnixMilli())

	verID := uuid.New()
	fd := &fakeDesired{states: []desired.State{{
		DeploymentID: uuid.New(), ServiceID: uuid.New(), VersionID: verID,
		Image: "nginx:alpine", Port: 80, Replicas: 3,
	}}}
	fa := &fakeActual{snap: map[string]observer.ObservedContainer{}}

	gw := gwclient.NewGatewayClient("", "") // 未接入 Gateway：drain 跳过
	r := New(fd, fa, reg, gw, time.Second, zap.NewNop())
	r.Reconcile(context.Background())

	mu.Lock()
	defer mu.Unlock()
	if created != 3 {
		t.Errorf("created = %d, want 3", created)
	}
}

// 观测滞后场景：连续两轮 reconcile 之间实际态快照尚未反映首轮创建的容器，
// in-flight 计数应阻止第二轮重复创建（否则会超配到 3）。
func TestReconcileNoOverProvisionOnObserveLag(t *testing.T) {
	var mu sync.Mutex
	created := 0
	agent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/api/internal/containers" {
			mu.Lock()
			created++
			mu.Unlock()
			_, _ = w.Write([]byte(`{"id":"c","instance_id":"x","host_port":9001}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer agent.Close()

	reg := agentregistry.New([]config.NodeConfig{{Name: "n1", Host: "10.0.0.1", AgentAddr: agent.URL}})
	reg.SetHealth("n1", true, "gw-1", time.Now().UnixMilli())

	fd := &fakeDesired{states: []desired.State{{
		DeploymentID: uuid.New(), ServiceID: uuid.New(), VersionID: uuid.New(),
		Image: "nginx:alpine", Port: 80, Replicas: 2,
	}}}
	// 实际态始终为空（模拟观测尚未确认）。
	fa := &fakeActual{snap: map[string]observer.ObservedContainer{}}
	gw := gwclient.NewGatewayClient("", "")
	r := New(fd, fa, reg, gw, time.Second, zap.NewNop())

	r.Reconcile(context.Background())
	r.Reconcile(context.Background())
	r.Reconcile(context.Background())

	mu.Lock()
	defer mu.Unlock()
	if created != 2 {
		t.Errorf("created = %d, want 2 (in-flight should suppress over-provisioning)", created)
	}
}

func TestReconcileScaleDownDrainsFirst(t *testing.T) {
	var mu sync.Mutex
	drained := false
	stopped, removed := 0, 0

	agent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/internal/containers/i1/stop":
			stopped++
		case r.Method == http.MethodDelete && r.URL.Path == "/api/internal/containers/i1":
			removed++
		}
		_, _ = w.Write([]byte(`{"stopped":true}`))
	}))
	defer agent.Close()

	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if r.URL.Path == "/api/internal/instances/i1/drain" {
			drained = true
		}
		_, _ = w.Write([]byte(`{"code":"OK"}`))
	}))
	defer gateway.Close()

	reg := agentregistry.New([]config.NodeConfig{{Name: "n1", Host: "10.0.0.1", AgentAddr: agent.URL}})
	reg.SetHealth("n1", true, "gw-node-1", time.Now().UnixMilli())

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
	if stopped != 1 || removed != 1 {
		t.Errorf("stopped=%d removed=%d, want 1/1", stopped, removed)
	}
}
