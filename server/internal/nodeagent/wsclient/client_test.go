package wsclient

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/NeoPlayful/maple-gateway/server/internal/agentprotocol"
	"go.uber.org/zap"
)

// fakeConn 是用通道驱动的假连接：写出的消息进 sent，读入来自 in。
type fakeConn struct {
	sent   chan agentprotocol.Envelope
	in     chan agentprotocol.Envelope
	closed chan struct{}
	once   sync.Once
}

func newFakeConn() *fakeConn {
	return &fakeConn{
		sent:   make(chan agentprotocol.Envelope, 16),
		in:     make(chan agentprotocol.Envelope, 16),
		closed: make(chan struct{}),
	}
}

func (c *fakeConn) WriteEnvelope(env agentprotocol.Envelope) error {
	select {
	case c.sent <- env:
		return nil
	case <-c.closed:
		return context.Canceled
	}
}

func (c *fakeConn) ReadEnvelope() (agentprotocol.Envelope, error) {
	select {
	case env := <-c.in:
		return env, nil
	case <-c.closed:
		return agentprotocol.Envelope{}, context.Canceled
	}
}

func (c *fakeConn) SetReadDeadline(time.Time) error  { return nil }
func (c *fakeConn) SetWriteDeadline(time.Time) error { return nil }
func (c *fakeConn) Close() error {
	c.once.Do(func() { close(c.closed) })
	return nil
}

// fakeDialer 返回预先建好的假连接。
type fakeDialer struct{ conn *fakeConn }

func (d *fakeDialer) Dial(context.Context, string) (Conn, error) { return d.conn, nil }

func TestHandshakeSavesCredentialAndExecutesTask(t *testing.T) {
	conn := newFakeConn()
	var saved *Credential
	enrollee := NewStaticEnrollee("ws://cm/agent/ws", "mg_enroll_x", "node-a", nil, func(c *Credential) error {
		saved = c
		return nil
	})
	done := make(chan struct{})
	exec := ExecutorFunc(func(_ context.Context, action string, _ json.RawMessage) (json.RawMessage, error) {
		close(done)
		return json.RawMessage(`{"ok":true}`), nil
	})
	c := New(enrollee, exec, &fakeDialer{conn: conn}, zap.NewNop())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go c.Run(ctx)

	// 校验首帧为 agent.hello 且带 enrollment token。
	hello := recvType(t, conn.sent, agentprotocol.TypeAgentHello)
	var hp agentprotocol.HelloPayload
	if err := hello.DecodePayload(&hp); err != nil {
		t.Fatalf("decode hello: %v", err)
	}
	if hp.EnrollmentToken != "mg_enroll_x" {
		t.Fatalf("want enrollment token, got %q", hp.EnrollmentToken)
	}

	// CM 回 agent.ready，下发凭证与心跳周期。
	ready, _ := agentprotocol.New(agentprotocol.TypeAgentReady, "", agentprotocol.ReadyPayload{
		NodeID: "node-1", NodeCredential: "mg_node_secret", HeartbeatSec: 1,
	})
	conn.in <- ready

	// 等待凭证被保存。
	waitFor(t, func() bool { return saved != nil })
	if saved.NodeID != "node-1" || saved.NodeSecret != "mg_node_secret" {
		t.Fatalf("credential not saved correctly: %+v", saved)
	}

	// 下发一个任务，Agent 应执行并回报 ack 与 result。
	exec1, _ := agentprotocol.New(agentprotocol.TypeTaskExecute, "req-1", agentprotocol.TaskExecutePayload{
		TaskID: "task-1", Action: agentprotocol.ActionContainerList,
	})
	conn.in <- exec1

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("executor not invoked")
	}
	// 收到 ack。
	ack := recvType(t, conn.sent, agentprotocol.TypeTaskAck)
	var ap agentprotocol.TaskAckPayload
	_ = ack.DecodePayload(&ap)
	if ap.TaskID != "task-1" {
		t.Fatalf("ack task mismatch: %s", ap.TaskID)
	}
	// 收到 result。
	res := recvType(t, conn.sent, agentprotocol.TypeTaskResult)
	var rp agentprotocol.TaskResultPayload
	if err := res.DecodePayload(&rp); err != nil {
		t.Fatalf("decode result: %v", err)
	}
	if rp.Status != "success" {
		t.Fatalf("want success, got %s (%s)", rp.Status, rp.Error)
	}
}

func TestForbiddenActionRejected(t *testing.T) {
	conn := newFakeConn()
	enrollee := NewStaticEnrollee("ws://cm", "", "n", nil, nil)
	exec := ExecutorFunc(func(context.Context, string, json.RawMessage) (json.RawMessage, error) {
		t.Fatal("executor must not run forbidden action")
		return nil, nil
	})
	c := New(enrollee, exec, &fakeDialer{conn: conn}, zap.NewNop())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go c.Run(ctx)

	recvType(t, conn.sent, agentprotocol.TypeAgentHello)
	ready, _ := agentprotocol.New(agentprotocol.TypeAgentReady, "", agentprotocol.ReadyPayload{NodeID: "n", HeartbeatSec: 60})
	conn.in <- ready

	exec1, _ := agentprotocol.New(agentprotocol.TypeTaskExecute, "", agentprotocol.TaskExecutePayload{
		TaskID: "t", Action: "shell.exec",
	})
	conn.in <- exec1

	res := recvType(t, conn.sent, agentprotocol.TypeTaskResult)
	var rp agentprotocol.TaskResultPayload
	_ = res.DecodePayload(&rp)
	if rp.Status != "failed" {
		t.Fatalf("forbidden action should fail, got %s", rp.Status)
	}
}

func TestCredentialRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/credential.json"
	if c, err := LoadCredential(path); err != nil || c != nil {
		t.Fatalf("missing file should return nil,nil: %v %v", c, err)
	}
	want := &Credential{NodeID: "n1", NodeSecret: "s1"}
	if err := SaveCredential(path, want); err != nil {
		t.Fatalf("save: %v", err)
	}
	got, err := LoadCredential(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got.NodeID != want.NodeID || got.NodeSecret != want.NodeSecret {
		t.Fatalf("round trip mismatch: %+v", got)
	}
}

func recvType(t *testing.T, ch <-chan agentprotocol.Envelope, typ agentprotocol.MessageType) agentprotocol.Envelope {
	t.Helper()
	deadline := time.After(2 * time.Second)
	for {
		select {
		case env := <-ch:
			if env.Type == typ {
				return env
			}
		case <-deadline:
			t.Fatalf("timed out waiting for %s", typ)
		}
	}
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.After(2 * time.Second)
	for !cond() {
		select {
		case <-deadline:
			t.Fatal("condition not met in time")
		case <-time.After(5 * time.Millisecond):
		}
	}
}
