package cmclient

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
)

func TestPushDeploy(t *testing.T) {
	var gotPath, gotAuth string
	var gotBody DesiredState
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := New(srv.URL, "tok")
	dep := uuid.New()
	err := c.PushDeploy(context.Background(), DesiredState{
		DeploymentID: dep, ServiceID: uuid.New(), VersionID: uuid.New(),
		Version: "v1", Image: "nginx:alpine", Replicas: 2, Port: 8080,
	})
	if err != nil {
		t.Fatalf("PushDeploy: %v", err)
	}
	if gotPath != "/api/deployments" {
		t.Errorf("path = %q", gotPath)
	}
	if gotAuth != "Bearer tok" {
		t.Errorf("auth = %q", gotAuth)
	}
	if gotBody.Replicas != 2 || gotBody.Image != "nginx:alpine" {
		t.Errorf("body = %+v", gotBody)
	}
}

func TestDisabledIsNoOp(t *testing.T) {
	c := New("", "tok")
	if c.Enabled() {
		t.Fatal("empty baseURL should be disabled")
	}
	if err := c.PushDeploy(context.Background(), DesiredState{}); err != nil {
		t.Errorf("disabled PushDeploy should be no-op, got %v", err)
	}
	if err := c.StopDeploy(context.Background(), uuid.New()); err != nil {
		t.Errorf("disabled StopDeploy should be no-op, got %v", err)
	}
}
