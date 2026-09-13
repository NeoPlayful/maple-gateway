package enrollment

import (
	"context"
	"testing"
	"time"

	"github.com/NeoPlayful/maple-gateway/server/internal/containermanager/agentconn"
	"github.com/NeoPlayful/maple-gateway/server/internal/containermanager/nodes"
)

// fakeRegistrar 把节点名解析为固定 UUID（模拟 Gateway get-or-create）。
type fakeRegistrar struct{ id string }

func (f fakeRegistrar) ResolveNode(context.Context, string, string, string, map[string]string) (string, error) {
	return f.id, nil
}

func (f fakeRegistrar) HeartbeatNode(context.Context, string) error { return nil }

func newMgr(requireEn bool, nodeID string) *Manager {
	return NewManager(requireEn, nodes.New(nil, 0, 0), fakeRegistrar{id: nodeID})
}

func TestEnrollThenReconnect(t *testing.T) {
	m := newMgr(true, "11111111-1111-1111-1111-111111111111")
	tok, err := m.Tokens().Issue("test", time.Hour)
	if err != nil {
		t.Fatalf("issue token: %v", err)
	}

	res, err := m.Authenticate(context.Background(), agentconn.Hello{
		EnrollmentToken: tok.Value, Hostname: "n1", OS: "linux",
	})
	if err != nil {
		t.Fatalf("first enroll: %v", err)
	}
	if res.NodeID == "" || res.Credential == "" || !res.NeedCredential {
		t.Fatalf("enroll result incomplete: %+v", res)
	}

	// Token 一次性：再次使用应被拒。
	if _, err := m.Authenticate(context.Background(), agentconn.Hello{EnrollmentToken: tok.Value, Hostname: "n1"}); err == nil {
		t.Fatal("reused token should be rejected")
	}

	// 重连：正确凭证放行。
	again, err := m.Authenticate(context.Background(), agentconn.Hello{NodeID: res.NodeID, Credential: res.Credential})
	if err != nil {
		t.Fatalf("reconnect with valid credential: %v", err)
	}
	if again.NodeID != res.NodeID {
		t.Fatalf("reconnect node mismatch: %s vs %s", again.NodeID, res.NodeID)
	}

	// 重连：错误凭证被拒。
	if _, err := m.Authenticate(context.Background(), agentconn.Hello{NodeID: res.NodeID, Credential: "wrong"}); err == nil {
		t.Fatal("reconnect with bad credential should be rejected")
	}
}

func TestRevokeCredential(t *testing.T) {
	m := newMgr(true, "11111111-1111-1111-1111-111111111111")
	res, err := m.Authenticate(context.Background(), agentconn.Hello{EnrollmentToken: mustIssue(t, m).Value, Hostname: "n1"})
	if err != nil {
		t.Fatalf("enroll: %v", err)
	}
	if !m.Nodes().Revoke(context.Background(), res.NodeID) {
		t.Fatal("revoke should succeed")
	}
	if _, err := m.Authenticate(context.Background(), agentconn.Hello{NodeID: res.NodeID, Credential: res.Credential}); err == nil {
		t.Fatal("revoked node should be rejected")
	}
}

func TestExpiredToken(t *testing.T) {
	m := newMgr(true, "11111111-1111-1111-1111-111111111111")
	tok := mustIssue(t, m)
	tok.ExpiresMs = time.Now().Add(-time.Minute).UnixMilli() // 置为已过期
	if _, err := m.Authenticate(context.Background(), agentconn.Hello{EnrollmentToken: tok.Value, Hostname: "n1"}); err == nil {
		t.Fatal("expired token should be rejected")
	}
}

func TestPermissiveMode(t *testing.T) {
	m := newMgr(false, "22222222-2222-2222-2222-222222222222")
	res, err := m.Authenticate(context.Background(), agentconn.Hello{Hostname: "x"})
	if err != nil {
		t.Fatalf("permissive enroll: %v", err)
	}
	if res.NodeID != "22222222-2222-2222-2222-222222222222" {
		t.Fatalf("permissive node mismatch: %s", res.NodeID)
	}
	if err := m.Verify("22222222-2222-2222-2222-222222222222", "anything"); err != nil {
		t.Fatalf("permissive verify should pass: %v", err)
	}
}

func mustIssue(t *testing.T, m *Manager) *Token {
	t.Helper()
	tok, err := m.Tokens().Issue("t", time.Hour)
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	return tok
}
