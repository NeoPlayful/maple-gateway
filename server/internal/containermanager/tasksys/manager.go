package tasksys

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/NeoPlayful/maple-gateway/server/internal/agentprotocol"
	"go.uber.org/zap"
)

// ErrUnknownTask 表示任务不存在。
var ErrUnknownTask = errors.New("unknown task")

// ErrNotCancellable 表示任务已终态，不能取消。
var ErrNotCancellable = errors.New("task not cancellable")

// defaultTimeout 是未指定超时时的缺省执行时限。
const defaultTimeout = 5 * time.Minute

// Manager 管理任务全生命周期与下发。
type Manager struct {
	router  Router
	timeout time.Duration
	logger  *zap.Logger

	mu    sync.RWMutex
	tasks map[string]*Task
}

// New 构造任务管理器。
func New(router Router, timeout time.Duration, logger *zap.Logger) *Manager {
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	return &Manager{
		router:  router,
		timeout: timeout,
		logger:  logger,
		tasks:   make(map[string]*Task),
	}
}

// Dispatch 创建任务并立即下发，返回任务快照。
// 下发失败时任务置 failed（无在线节点/投递失败），但记录仍保留可查。
func (m *Manager) Dispatch(nodeID, action string, params any) (*Task, error) {
	if !agentprotocol.IsAllowedAction(action) {
		return nil, fmt.Errorf("action %q not allowed", action)
	}
	var raw json.RawMessage
	if params != nil {
		b, err := json.Marshal(params)
		if err != nil {
			return nil, err
		}
		raw = b
	}
	id, err := randomID("task_")
	if err != nil {
		return nil, err
	}
	t := newTask()
	t.ID = id
	t.NodeID = nodeID
	t.Action = action
	t.Params = raw
	t.Status = StatusPending
	t.RequestID = id
	t.CreatedMs = time.Now().UnixMilli()
	t.DeadlineMs = time.Now().Add(m.timeout).UnixMilli()
	m.mu.Lock()
	m.tasks[id] = t
	m.mu.Unlock()

	sender, ok := m.router.SenderFor(nodeID)
	if !ok {
		t.mu.Lock()
		t.Error = "node offline"
		t.setStatus(StatusFailed)
		t.mu.Unlock()
		return t.Snapshot(), fmt.Errorf("node %s offline", nodeID)
	}

	env, _ := agentprotocol.New(agentprotocol.TypeTaskExecute, t.RequestID, agentprotocol.TaskExecutePayload{
		TaskID: t.ID, Action: action, Params: raw,
	})
	t.mu.Lock()
	t.Attempts++
	t.mu.Unlock()
	if !sender.Send(env) {
		t.mu.Lock()
		t.Error = "send failed"
		t.setStatus(StatusFailed)
		t.mu.Unlock()
		return t.Snapshot(), fmt.Errorf("send task to node %s failed", nodeID)
	}
	t.mu.Lock()
	t.setStatus(StatusDispatching)
	t.mu.Unlock()
	return t.Snapshot(), nil
}

// Get 取任务快照。
func (m *Manager) Get(id string) (*Task, bool) {
	m.mu.RLock()
	t, ok := m.tasks[id]
	m.mu.RUnlock()
	if !ok {
		return nil, false
	}
	return t.Snapshot(), true
}

// Call 同步下发一次任务并等待其终态，返回结果的原始载荷。
// 用于请求/响应式操作（探测、列表、人工启停）。超时或失败返回已定型的错误。
func (m *Manager) Call(ctx context.Context, nodeID, action string, params any) (json.RawMessage, error) {
	t, err := m.Dispatch(nodeID, action, params)
	if err != nil {
		return nil, err
	}
	// Dispatch 已返回快照；按 ID 取回内部指针以等待终态。
	m.mu.RLock()
	live, ok := m.tasks[t.ID]
	m.mu.RUnlock()
	if !ok {
		return nil, ErrUnknownTask
	}
	select {
	case <-live.done:
	case <-ctx.Done():
		// 上下文取消：尽力取消任务，返回上下文错误。
		m.Cancel(nodeID, t.ID, "context canceled")
		return nil, ctx.Err()
	}
	got, _ := m.Get(t.ID)
	if got.Status != StatusSuccess {
		if got.Error != "" {
			return nil, fmt.Errorf("task %s: %s", got.Status, got.Error)
		}
		return nil, fmt.Errorf("task %s", got.Status)
	}
	return got.Result, nil
}

// List 返回全部任务快照。
func (m *Manager) List() []*Task {
	m.mu.RLock()
	ts := make([]*Task, 0, len(m.tasks))
	for _, t := range m.tasks {
		ts = append(ts, t)
	}
	m.mu.RUnlock()
	out := make([]*Task, 0, len(ts))
	for _, t := range ts {
		out = append(out, t.Snapshot())
	}
	return out
}

// OnAck 处理 Agent 接收：dispatching → running。幂等：非 dispatching 忽略。
func (m *Manager) OnAck(nodeID, taskID string) {
	m.transition(nodeID, taskID, StatusDispatching, StatusRunning, "")
}

// OnProgress 更新进度（仅运行中生效）。
func (m *Manager) OnProgress(nodeID, taskID string, percent int, msg string) {
	t := m.lookup(nodeID, taskID)
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.Status != StatusRunning && t.Status != StatusDispatching {
		return
	}
	if percent >= 0 {
		t.Percent = percent
	}
	if msg != "" {
		t.Message = msg
	}
}

// OnResult 处理执行结果：非终态任务收敛到 success/failed。
func (m *Manager) OnResult(nodeID, taskID, status, errMsg string, result json.RawMessage) {
	t := m.lookup(nodeID, taskID)
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.Status.Terminal() {
		return // 重复结果不覆盖终态
	}
	t.Result = result
	t.Error = errMsg
	if status == string(StatusSuccess) {
		t.Percent = 100
		t.setStatus(StatusSuccess)
	} else {
		t.setStatus(StatusFailed)
	}
}

// Cancel 取消任务并向下发取消指令。仅非终态可取消。
func (m *Manager) Cancel(nodeID, taskID, reason string) error {
	t := m.lookup(nodeID, taskID)
	if t == nil {
		return ErrUnknownTask
	}
	t.mu.Lock()
	if t.Status.Terminal() {
		t.mu.Unlock()
		return ErrNotCancellable
	}
	t.Error = reason
	t.setStatus(StatusCancelled)
	t.mu.Unlock()

	if sender, ok := m.router.SenderFor(nodeID); ok {
		env, _ := agentprotocol.New(agentprotocol.TypeTaskCancel, "", agentprotocol.TaskCancelPayload{
			TaskID: taskID, Reason: reason,
		})
		sender.Send(env)
	}
	return nil
}

// Sweep 扫描超时任务：非终态且已过 deadline，置 timeout，并尽力向 Agent 下发
// task.cancel 做补偿，避免 CM 判超时、Agent 仍在执行导致两侧分叉。
func (m *Manager) Sweep() int {
	now := time.Now().UnixMilli()
	n := 0
	for _, t := range m.snapshotTasks() {
		t.mu.Lock()
		expired := !t.Status.Terminal() && t.DeadlineMs > 0 && now >= t.DeadlineMs
		if expired {
			t.Error = "task timeout"
			t.setStatus(StatusTimeout)
			n++
		}
		nodeID := t.NodeID
		taskID := t.ID
		t.mu.Unlock()
		if expired {
			if sender, ok := m.router.SenderFor(nodeID); ok {
				env, _ := agentprotocol.New(agentprotocol.TypeTaskCancel, "", agentprotocol.TaskCancelPayload{
					TaskID: taskID, Reason: "task timeout",
				})
				sender.Send(env)
			}
		}
	}
	return n
}

// RunSweeper 周期扫描超时任务，直到 ctx 相关信号触发（由调用方控制退出）。
func (m *Manager) RunSweeper(stop <-chan struct{}, interval time.Duration) {
	if interval <= 0 {
		interval = 15 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			if n := m.Sweep(); n > 0 {
				m.logger.Info("tasks timed out", zap.Int("count", n))
			}
		}
	}
}

// transition 执行一次受校验的状态迁移。
func (m *Manager) transition(nodeID, taskID string, from, to Status, errMsg string) {
	t := m.lookup(nodeID, taskID)
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.Status != from {
		return
	}
	if errMsg != "" {
		t.Error = errMsg
	}
	t.setStatus(to)
}

// lookup 按节点与任务 ID 取任务（校验归属，防止跨节点误操作）。
func (m *Manager) lookup(nodeID, taskID string) *Task {
	m.mu.RLock()
	t, ok := m.tasks[taskID]
	m.mu.RUnlock()
	if !ok || t.NodeID != nodeID {
		return nil
	}
	return t
}

// snapshotTasks 返回内部任务指针切片（仅用于内部遍历，调用方自行加锁）。
func (m *Manager) snapshotTasks() []*Task {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*Task, 0, len(m.tasks))
	for _, t := range m.tasks {
		out = append(out, t)
	}
	return out
}

func randomID(prefix string) (string, error) {
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return prefix + hex.EncodeToString(b), nil
}
