package enrollment

import (
	"testing"
	"time"

	"github.com/NeoPlayful/maple-gateway/server/internal/containermanager/agentconn"
)

func TestEnrollThenReconnect(t *testing.T) {
	m := NewManager(true)
	tok, err := m.Tokens().Issue("test", time.Hour)
	if err != nil {
		t.Fatalf("issue token: %v", err)
	}

	res, err := m.Authenticate(agentconn.Hello{
		EnrollmentToken: tok.Value, Hostname: "n1", OS: "linux",
	})
	if err != nil {
		t.Fatalf("first enroll: %v", err)
	}
	if res.NodeID == "" || res.Credential == "" || !res.NeedCredential {
		t.Fatalf("enroll result incomplete: %+v", res)
	}

	// Token 一次性：再次使用应被拒。
	if _, err := m.Authenticate(agentconn.Hello{EnrollmentToken: tok.Value}); err == nil {
		t.Fatal("reused token should be rejected")
	}

	// 重连：正确凭证放行。
	again, err := m.Authenticate(agentconn.Hello{NodeID: res.NodeID, Credential: res.Credential})
	if err != nil {
		t.Fatalf("reconnect with valid credential: %v", err)
	}
	if again.NodeID != res.NodeID {
		t.Fatalf("reconnect node mismatch: %s vs %s", again.NodeID, res.NodeID)
	}

	// 重连：错误凭证被拒。
	if _, err := m.Authenticate(agentconn.Hello{NodeID: res.NodeID, Credential: "wrong"}); err == nil {
		t.Fatal("reconnect with bad credential should be rejected")
	}
}

func TestRevokeCredential(t *testing.T) {
	m := NewManager(true)
	res, err := m.Authenticate(agentconn.Hello{EnrollmentToken: mustIssue(t, m).Value})
	if err != nil {
		t.Fatalf("enroll: %v", err)
	}
	if !m.Nodes().Revoke(res.NodeID) {
		t.Fatal("revoke should succeed")
	}
	if _, err := m.Authenticate(agentconn.Hello{NodeID: res.NodeID, Credential: res.Credential}); err == nil {
		t.Fatal("revoked node should be rejected")
	}
}

func TestExpiredToken(t *testing.T) {
	m := NewManager(true)
	tok := mustIssue(t, m)
	tok.ExpiresMs = time.Now().Add(-time.Minute).UnixMilli() // 置为已过期
	if _, err := m.Authenticate(agentconn.Hello{EnrollmentToken: tok.Value}); err == nil {
		t.Fatal("expired token should be rejected")
	}
}

func TestPermissiveMode(t *testing.T) {
	m := NewManager(false)
	res, err := m.Authenticate(agentconn.Hello{NodeID: "node-x", Hostname: "x"})
	if err != nil {
		t.Fatalf("permissive reconnect: %v", err)
	}
	if res.NodeID != "node-x" {
		t.Fatalf("permissive node mismatch: %s", res.NodeID)
	}
	if err := m.Verify("node-x", "anything"); err != nil {
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
