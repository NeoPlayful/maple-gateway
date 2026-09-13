// Package tasksys 承载 CM 侧的任务系统：把一次 Agent 操作建模为有状态的 Task，
// 经 Agent 会话下发（task.execute），并依据 ack/progress/result 收敛状态。
//
// 任务与「期望态对账」并存：对账负责把系统推向目标形态（幂等、可重放），
// Task 负责一次性动作（探测、拉镜像、人工启停）与可观测的执行结果。
package tasksys

import (
	"encoding/json"
	"sync"
	"time"
)

// Status 是任务状态。
type Status string

const (
	StatusPending     Status = "pending"     // 已创建，待下发
	StatusDispatching Status = "dispatching" // 已下发，待 Agent 接收
	StatusRunning     Status = "running"     // Agent 已接收，执行中
	StatusSuccess     Status = "success"     // 执行成功（终态）
	StatusFailed      Status = "failed"      // 执行失败（终态）
	StatusCancelled   Status = "cancelled"   // 已取消（终态）
	StatusTimeout     Status = "timeout"     // 超时（终态）
)

// Terminal 报告状态是否为终态（不再变化）。
func (s Status) Terminal() bool {
	switch s {
	case StatusSuccess, StatusFailed, StatusCancelled, StatusTimeout:
		return true
	default:
		return false
	}
}

// Task 是一次下发任务的记录。
type Task struct {
	ID         string          `json:"id"`
	NodeID     string          `json:"node_id"`
	Action     string          `json:"action"`
	Params     json.RawMessage `json:"params,omitempty"`
	Status     Status          `json:"status"`
	Message    string          `json:"message,omitempty"`
	Percent    int             `json:"percent,omitempty"`
	Error      string          `json:"error,omitempty"`
	Result     json.RawMessage `json:"result,omitempty"`
	RequestID  string          `json:"request_id,omitempty"`
	CreatedMs  int64           `json:"created_at_ms"`
	StartedMs  int64           `json:"started_at_ms,omitempty"`
	FinishedMs int64           `json:"finished_at_ms,omitempty"`
	DeadlineMs int64           `json:"deadline_ms,omitempty"`
	Attempts   int             `json:"attempts"`
	mu         *sync.Mutex     `json:"-"`
	done       chan struct{}   `json:"-"` // 终态时关闭，供同步等待者唤醒
	finished   bool            `json:"-"` // 是否已关闭 done（保证只关一次）
}

// newTask 构造任务并初始化其锁与终态信号。
func newTask() *Task { return &Task{mu: &sync.Mutex{}, done: make(chan struct{})} }

// setStatus 在锁内迁移状态并记录时间戳。
func (t *Task) setStatus(s Status) {
	t.Status = s
	now := time.Now().UnixMilli()
	if s == StatusRunning && t.StartedMs == 0 {
		t.StartedMs = now
	}
	if s.Terminal() {
		t.FinishedMs = now
		if !t.finished {
			t.finished = true
			close(t.done)
		}
	}
}

// Snapshot 返回任务的只读副本（供外发）。
func (t *Task) Snapshot() *Task {
	t.mu.Lock()
	defer t.mu.Unlock()
	cp := *t
	return &cp
}
