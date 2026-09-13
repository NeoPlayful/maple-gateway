package tasksys

import (
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/NeoPlayful/maple-gateway/server/internal/agentprotocol"
	"go.uber.org/zap"
)

// fakeRouter 记录下发消息并返回一个始终可用的发送通道。
type fakeRouter struct {
	mu     sync.Mutex
	sent   []agentprotocol.Envelope
	online map[string]bool
}

func newFakeRouter() *fakeRouter {
	return &fakeRouter{online: map[string]bool{"node-1": true}}
}

func (f *fakeRouter) SenderFor(nodeID string) (Sender, bool) {
	if !f.online[nodeID] {
		return nil, false
	}
	return SenderFunc(func(env agentprotocol.Envelope) bool {
		f.mu.Lock()
		f.sent = append(f.sent, env)
		f.mu.Unlock()
		return true
	}), true
}

func (f *fakeRouter) count(typ agentprotocol.MessageType) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, e := range f.sent {
		if e.Type == typ {
			n++
		}
	}
	return n
}

func newMgr(t *testing.T, r Router) *Manager {
	t.Helper()
	return New(r, time.Minute, zap.NewNop())
}

func TestDispatchLifecycle(t *testing.T) {
	r := newFakeRouter()
	m := newMgr(t, r)

	task, err := m.Dispatch("node-1", agentprotocol.ActionContainerStart, map[string]string{"id": "c1"})
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if task.Status != StatusDispatching {
		t.Fatalf("want dispatching, got %s", task.Status)
	}
	if r.count(agentprotocol.TypeTaskExecute) != 1 {
		t.Fatal("expected one task.execute sent")
	}

	m.OnAck("node-1", task.ID)
	m.OnProgress("node-1", task.ID, 50, "halfway")
	m.OnResult("node-1", task.ID, string(StatusSuccess), "", json.RawMessage(`{"ok":true}`))

	got, _ := m.Get(task.ID)
	if got.Status != StatusSuccess {
		t.Fatalf("want success, got %s", got.Status)
	}
	if got.Percent != 100 {
		t.Fatalf("want percent 100, got %d", got.Percent)
	}
}

func TestResultIdempotent(t *testing.T) {
	r := newFakeRouter()
	m := newMgr(t, r)
	task, _ := m.Dispatch("node-1", agentprotocol.ActionSystemInfo, nil)
	m.OnAck("node-1", task.ID)
	m.OnResult("node-1", task.ID, string(StatusSuccess), "", nil)
	// 迟到的失败结果不得覆盖已成功的终态。
	m.OnResult("node-1", task.ID, string(StatusFailed), "late", nil)
	got, _ := m.Get(task.ID)
	if got.Status != StatusSuccess {
		t.Fatalf("terminal state overwritten: %s", got.Status)
	}
}

func TestDispatchOfflineNode(t *testing.T) {
	r := newFakeRouter()
	m := newMgr(t, r)
	task, err := m.Dispatch("node-missing", agentprotocol.ActionSystemInfo, nil)
	if err == nil {
		t.Fatal("expected error for offline node")
	}
	if task.Status != StatusFailed {
		t.Fatalf("want failed, got %s", task.Status)
	}
}

func TestCancel(t *testing.T) {
	r := newFakeRouter()
	m := newMgr(t, r)
	task, _ := m.Dispatch("node-1", agentprotocol.ActionImagePull, map[string]string{"image": "nginx"})
	if err := m.Cancel("node-1", task.ID, "user abort"); err != nil {
		t.Fatalf("cancel: %v", err)
	}
	got, _ := m.Get(task.ID)
	if got.Status != StatusCancelled {
		t.Fatalf("want cancelled, got %s", got.Status)
	}
	if r.count(agentprotocol.TypeTaskCancel) != 1 {
		t.Fatal("expected one task.cancel sent")
	}
	// 终态不可再次取消。
	if err := m.Cancel("node-1", task.ID, "again"); err != ErrNotCancellable {
		t.Fatalf("want ErrNotCancellable, got %v", err)
	}
}

func TestTimeoutSweep(t *testing.T) {
	r := newFakeRouter()
	m := New(r, time.Millisecond, zap.NewNop())
	task, _ := m.Dispatch("node-1", agentprotocol.ActionContainerList, nil)
	time.Sleep(5 * time.Millisecond)
	if n := m.Sweep(); n != 1 {
		t.Fatalf("want 1 timed out, got %d", n)
	}
	got, _ := m.Get(task.ID)
	if got.Status != StatusTimeout {
		t.Fatalf("want timeout, got %s", got.Status)
	}
}

func TestDisallowedAction(t *testing.T) {
	r := newFakeRouter()
	m := newMgr(t, r)
	if _, err := m.Dispatch("node-1", "shell.exec", nil); err == nil {
		t.Fatal("forbidden action should be rejected")
	}
}

func TestRetryLineage(t *testing.T) {
	r := newFakeRouter()
	m := newMgr(t, r)
	task, _ := m.DispatchBy("node-1", agentprotocol.ActionImagePull, map[string]string{"image": "nginx"}, "admin-1")
	m.OnResult("node-1", task.ID, string(StatusFailed), "boom", nil)

	// 未结束任务不可重试。
	running, _ := m.Dispatch("node-1", agentprotocol.ActionSystemInfo, nil)
	if _, err := m.Retry(running.ID); err == nil {
		t.Fatal("non-terminal task should not be retryable")
	}

	retried, err := m.Retry(task.ID)
	if err != nil {
		t.Fatalf("retry: %v", err)
	}
	if retried.ID == task.ID {
		t.Fatal("retry should create a new task id")
	}
	if retried.ParentID != task.ID {
		t.Fatalf("want parent %s, got %s", task.ID, retried.ParentID)
	}
	if retried.CreatedBy != "admin-1" {
		t.Fatalf("retry should inherit creator, got %q", retried.CreatedBy)
	}
	if retried.Attempts != task.Attempts+1 {
		t.Fatalf("want attempts %d, got %d", task.Attempts+1, retried.Attempts)
	}
	if retried.Status != StatusDispatching {
		t.Fatalf("want dispatching, got %s", retried.Status)
	}
}

func TestAdminCancelUnknown(t *testing.T) {
	r := newFakeRouter()
	m := newMgr(t, r)
	if err := m.AdminCancel("nope", "x"); err != ErrUnknownTask {
		t.Fatalf("want ErrUnknownTask, got %v", err)
	}
	task, _ := m.Dispatch("node-1", agentprotocol.ActionSystemInfo, nil)
	if err := m.AdminCancel(task.ID, "operator"); err != nil {
		t.Fatalf("admin cancel: %v", err)
	}
	got, _ := m.Get(task.ID)
	if got.Status != StatusCancelled {
		t.Fatalf("want cancelled, got %s", got.Status)
	}
}
