package tasksys

import (
	"context"
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

// 探测走 Probe：仍下发并取回结果，但不产生任务记录。
func TestProbeLeavesNoRecord(t *testing.T) {
	r := newFakeRouter()
	m := newMgr(t, r)

	// 另起一路等待 Probe 的结果（默认 fakeRouter 只记录发送、不回执）。
	go func() {
		// 等待 task.execute 到达后回一条成功结果，唤醒等待者。
		for r.count(agentprotocol.TypeTaskExecute) == 0 {
			time.Sleep(time.Millisecond)
		}
		r.mu.Lock()
		var taskID string
		for _, e := range r.sent {
			if e.Type == agentprotocol.TypeTaskExecute {
				var p agentprotocol.TaskExecutePayload
				_ = e.DecodePayload(&p)
				taskID = p.TaskID
			}
		}
		r.mu.Unlock()
		m.OnAck("node-1", taskID)
		m.OnResult("node-1", taskID, string(StatusSuccess), "", json.RawMessage(`{"ok":true}`))
	}()

	res, err := m.Probe(context.Background(), "node-1", agentprotocol.ActionContainerList, nil)
	if err != nil {
		t.Fatalf("probe: %v", err)
	}
	if string(res) != `{"ok":true}` {
		t.Fatalf("probe result = %s, want {\"ok\":true}", res)
	}
	// 探测不留痕：可查列表与分页均为空。
	if got := m.List(); len(got) != 0 {
		t.Fatalf("List after probe = %d tasks, want 0", len(got))
	}
	if _, total := m.Page("", 10, 0); total != 0 {
		t.Fatalf("Page total after probe = %d, want 0", total)
	}

	// 正常下发仍留记录（对照）。
	running, err := m.Dispatch("node-1", agentprotocol.ActionImagePull, map[string]string{"image": "nginx"})
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if _, ok := m.Get(running.ID); !ok {
		t.Fatal("dispatched task should remain visible")
	}
	if _, total := m.Page("", 10, 0); total != 1 {
		t.Fatalf("Page total after dispatch = %d, want 1", total)
	}
}

// 在途探测（已下发、尚未返回结果）对管理端不可见：列表/分页/详情都不应出现。
func TestInflightProbeInvisible(t *testing.T) {
	r := newFakeRouter()
	m := newMgr(t, r)

	done := make(chan error, 1)
	go func() {
		_, err := m.Probe(context.Background(), "node-1", agentprotocol.ActionContainerList, nil)
		done <- err
	}()

	// 等到探测已下发（此刻阻塞在 await 内）。
	var taskID string
	for i := 0; i < 3000 && taskID == ""; i++ {
		r.mu.Lock()
		for _, e := range r.sent {
			if e.Type == agentprotocol.TypeTaskExecute {
				var p agentprotocol.TaskExecutePayload
				_ = e.DecodePayload(&p)
				taskID = p.TaskID
			}
		}
		r.mu.Unlock()
		if taskID == "" {
			time.Sleep(time.Millisecond)
		}
	}
	if taskID == "" {
		t.Fatal("probe was not dispatched")
	}

	// 在途期间：对外三路均不可见。
	if got := m.List(); len(got) != 0 {
		t.Fatalf("in-flight probe visible in List: %d", len(got))
	}
	if _, total := m.Page("", 10, 0); total != 0 {
		t.Fatalf("in-flight probe counted in Page total: %d", total)
	}
	if _, ok := m.Get(taskID); ok {
		t.Fatal("in-flight probe visible via Get")
	}

	// 放行探测，确认过滤不影响结果取回。
	m.OnAck("node-1", taskID)
	m.OnResult("node-1", taskID, string(StatusSuccess), "", json.RawMessage(`{"ok":1}`))
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("probe: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("probe did not complete")
	}
}

func TestPageFiltersByNodeAndSlices(t *testing.T) {
	r := newFakeRouter()
	m := newMgr(t, r)

	// node-1 下发 3 条、node-2 下发 1 条（node-2 需在线，先补登记）。
	r.mu.Lock()
	r.online["node-2"] = true
	r.mu.Unlock()
	for i := 0; i < 3; i++ {
		if _, err := m.Dispatch("node-1", agentprotocol.ActionSystemInfo, nil); err != nil {
			t.Fatalf("dispatch node-1 #%d: %v", i, err)
		}
	}
	if _, err := m.Dispatch("node-2", agentprotocol.ActionContainerList, nil); err != nil {
		t.Fatalf("dispatch node-2: %v", err)
	}

	// 全量：total 为两节点合计，limit/offset 截取当页。
	all, total := m.Page("", 2, 0)
	if total != 4 {
		t.Fatalf("all total = %d, want 4", total)
	}
	if len(all) != 2 {
		t.Fatalf("all page len = %d, want 2", len(all))
	}
	// 按创建时间倒序（同毫秒按 ID 倒序）：相邻项不得升序。
	full, _ := m.Page("", 10, 0)
	for i := 1; i < len(full); i++ {
		prev, cur := full[i-1], full[i]
		if prev.CreatedMs < cur.CreatedMs ||
			(prev.CreatedMs == cur.CreatedMs && prev.ID < cur.ID) {
			t.Fatalf("page not ordered desc at %d: (%d,%s) then (%d,%s)",
				i, prev.CreatedMs, prev.ID, cur.CreatedMs, cur.ID)
		}
	}

	// 按节点过滤：仅统计 node-1 的 3 条。
	n1, total1 := m.Page("node-1", 10, 0)
	if total1 != 3 {
		t.Fatalf("node-1 total = %d, want 3", total1)
	}
	for _, tk := range n1 {
		if tk.NodeID != "node-1" {
			t.Fatalf("filtered task node = %s, want node-1", tk.NodeID)
		}
	}

	// 越界 offset 返回空页但仍给出 total。
	empty, total2 := m.Page("node-1", 10, 99)
	if len(empty) != 0 || total2 != 3 {
		t.Fatalf("out-of-range page = (len %d, total %d), want (0, 3)", len(empty), total2)
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
