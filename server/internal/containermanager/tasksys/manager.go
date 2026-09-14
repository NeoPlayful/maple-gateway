package tasksys

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
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
	store   *Store

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

// WithStore 挂载持久化存储（未配置数据库时可不调用）。
func (m *Manager) WithStore(store *Store) *Manager {
	m.store = store
	return m
}

// Load 从数据库装载历史任务到内存（无数据库时为无操作）。
// 已处于终态的历史任务只读回填；非终态任务在重启后不可能再收到 Agent 回执，
// 统一收敛为 interrupted（failed），避免永久停留在 dispatching/running。
func (m *Manager) Load(ctx context.Context) error {
	if m.store == nil {
		return nil
	}
	ts, err := m.store.load(ctx)
	if err != nil {
		return fmt.Errorf("load cm_tasks: %w", err)
	}
	m.mu.Lock()
	for _, t := range ts {
		if !t.Status.Terminal() {
			t.Error = "interrupted by cm restart"
			t.setStatus(StatusFailed)
		}
		m.tasks[t.ID] = t
	}
	m.mu.Unlock()
	for _, t := range ts {
		if t.Status == StatusFailed && t.Error == "interrupted by cm restart" {
			m.persist(t)
		}
	}
	return nil
}

// persist 把任务当前快照写穿到存储；store 为空或任务为临时（观测探测）时无操作。
func (m *Manager) persist(t *Task) {
	if m.store != nil && !t.ephemeral {
		m.store.Save(t)
	}
}

// Dispatch 创建任务并立即下发，返回任务快照。
// 下发失败时任务置 failed（无在线节点/投递失败），但记录仍保留可查。
func (m *Manager) Dispatch(nodeID, action string, params any) (*Task, error) {
	return m.DispatchBy(nodeID, action, params, "")
}

// DispatchBy 同 Dispatch，额外记录创建人（管理端触发的任务）。
func (m *Manager) DispatchBy(nodeID, action string, params any, createdBy string) (*Task, error) {
	return m.dispatch(nodeID, action, params, createdBy, false)
}

// dispatch 创建任务并下发；ephemeral 为 true 时不落库、不进可查列表。
func (m *Manager) dispatch(nodeID, action string, params any, createdBy string, ephemeral bool) (*Task, error) {
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
	t.CreatedBy = createdBy
	t.CreatedMs = time.Now().UnixMilli()
	t.DeadlineMs = time.Now().Add(m.timeout).UnixMilli()
	t.ephemeral = ephemeral
	m.mu.Lock()
	m.tasks[id] = t
	m.mu.Unlock()
	m.persist(t)
	return m.deliver(t, nodeID)
}

// Probe 下发一次系统探测：与 Call 一样等待终态并返回结果，但不产生任务记录——
// 不落库、不进可查列表，终态后即从内存表移除。供观测循环等系统动作取数据用，
// 避免每轮探测都留下任务记录。
func (m *Manager) Probe(ctx context.Context, nodeID, action string, params any) (json.RawMessage, error) {
	t, err := m.dispatch(nodeID, action, params, "", true)
	if err != nil {
		m.dropEphemeral(t)
		return nil, err
	}
	// dispatch 返回快照；取回内存表中的活指针，以便等待终态并读取其结果。
	m.mu.RLock()
	live, ok := m.tasks[t.ID]
	m.mu.RUnlock()
	if !ok {
		return nil, ErrUnknownTask
	}
	defer m.dropEphemeral(live)
	return m.await(ctx, nodeID, live)
}

// dropEphemeral 从内存表移除临时任务；非临时任务为无操作。
func (m *Manager) dropEphemeral(t *Task) {
	if t == nil || !t.ephemeral {
		return
	}
	m.mu.Lock()
	delete(m.tasks, t.ID)
	m.mu.Unlock()
}

// deliver 向节点投递一个已创建的任务，并在投递结果上收敛状态。
func (m *Manager) deliver(t *Task, nodeID string) (*Task, error) {
	sender, ok := m.router.SenderFor(nodeID)
	if !ok {
		t.mu.Lock()
		t.Error = "node offline"
		t.setStatus(StatusFailed)
		t.mu.Unlock()
		m.persist(t)
		return t.Snapshot(), fmt.Errorf("node %s offline", nodeID)
	}

	env, _ := agentprotocol.New(agentprotocol.TypeTaskExecute, t.RequestID, agentprotocol.TaskExecutePayload{
		TaskID: t.ID, Action: t.Action, Params: t.Params,
	})
	t.mu.Lock()
	t.Attempts++
	t.mu.Unlock()
	if !sender.Send(env) {
		t.mu.Lock()
		t.Error = "send failed"
		t.setStatus(StatusFailed)
		t.mu.Unlock()
		m.persist(t)
		return t.Snapshot(), fmt.Errorf("send task to node %s failed", nodeID)
	}
	t.mu.Lock()
	t.setStatus(StatusDispatching)
	t.mu.Unlock()
	m.persist(t)
	return t.Snapshot(), nil
}

// Retry 基于一个已终态（失败/超时/取消）任务重新下发一次。
// 新任务关联原任务的 action/params/node，并以 ParentID 记录谱系，Attempts 继承原值 +1。
func (m *Manager) Retry(id string) (*Task, error) {
	m.mu.RLock()
	orig, ok := m.tasks[id]
	m.mu.RUnlock()
	if !ok {
		return nil, ErrUnknownTask
	}
	snap := orig.Snapshot()
	if !snap.Status.Terminal() {
		return nil, fmt.Errorf("task %s 尚未结束，无法重试", id)
	}
	newID, err := randomID("task_")
	if err != nil {
		return nil, err
	}
	t := newTask()
	t.ID = newID
	t.NodeID = snap.NodeID
	t.Action = snap.Action
	t.Params = snap.Params
	t.Status = StatusPending
	t.RequestID = newID
	t.ParentID = snap.ID
	t.CreatedBy = snap.CreatedBy
	t.Attempts = snap.Attempts
	t.CreatedMs = time.Now().UnixMilli()
	t.DeadlineMs = time.Now().Add(m.timeout).UnixMilli()
	m.mu.Lock()
	m.tasks[newID] = t
	m.mu.Unlock()
	m.persist(t)
	return m.deliver(t, snap.NodeID)
}

// SenderFor 实现 agentregistry.Commander：暴露向节点单向投递的通道（日志流等异步指令）。
func (m *Manager) SenderFor(nodeID string) (Sender, bool) {
	return m.router.SenderFor(nodeID)
}

// Get 取任务快照。临时任务（观测探测）对管理端不可见，一律视为不存在。
func (m *Manager) Get(id string) (*Task, bool) {
	m.mu.RLock()
	t, ok := m.tasks[id]
	m.mu.RUnlock()
	if !ok || t.ephemeral {
		return nil, false
	}
	return t.Snapshot(), true
}

// Call 同步下发一次任务并等待其终态，返回结果的原始载荷。
// 用于请求/响应式操作（人工启停、列表等）；任务的记录与列表可见性由 dispatch 决定。
// 超时或失败返回已定型的错误。
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
	return m.await(ctx, nodeID, live)
}

// await 等待一个已下发任务到达终态并返回其结果。
// 上下文取消时尽力下发 task.cancel 做补偿，并返回上下文错误。
func (m *Manager) await(ctx context.Context, nodeID string, live *Task) (json.RawMessage, error) {
	select {
	case <-live.done:
	case <-ctx.Done():
		m.Cancel(nodeID, live.ID, "context canceled")
		return nil, ctx.Err()
	}
	// 直接读持有的任务快照：探测类临时任务对管理端 Get 已过滤，不能经其取回。
	got := live.Snapshot()
	if got.Status != StatusSuccess {
		if got.Error != "" {
			return nil, fmt.Errorf("task %s: %s", got.Status, got.Error)
		}
		return nil, fmt.Errorf("task %s", got.Status)
	}
	return got.Result, nil
}

// List 返回全部任务快照。临时任务（观测探测）对管理端不可见，不纳入结果。
func (m *Manager) List() []*Task {
	m.mu.RLock()
	ts := make([]*Task, 0, len(m.tasks))
	for _, t := range m.tasks {
		if t.ephemeral {
			continue
		}
		ts = append(ts, t)
	}
	m.mu.RUnlock()
	out := make([]*Task, 0, len(ts))
	for _, t := range ts {
		out = append(out, t.Snapshot())
	}
	return out
}

// Page 返回一页任务快照与匹配总数：按创建时间倒序（最新在前）截取 [offset, offset+limit)。
// nodeID 非空时仅统计/返回该节点的任务；nodeID 为空则返回全部节点的任务。
// 任务表无界增长，管理端一律走本方法取数，避免整表外发撑爆响应体。
func (m *Manager) Page(nodeID string, limit, offset int) ([]*Task, int) {
	m.mu.RLock()
	ts := make([]*Task, 0, len(m.tasks))
	for _, t := range m.tasks {
		if t.ephemeral {
			continue
		}
		ts = append(ts, t)
	}
	m.mu.RUnlock()
	if nodeID != "" {
		filtered := ts[:0]
		for _, t := range ts {
			if t.NodeID == nodeID {
				filtered = append(filtered, t)
			}
		}
		ts = filtered
	}
	sort.Slice(ts, func(i, j int) bool {
		if ts[i].CreatedMs != ts[j].CreatedMs {
			return ts[i].CreatedMs > ts[j].CreatedMs
		}
		return ts[i].ID > ts[j].ID
	})
	total := len(ts)
	if offset < 0 {
		offset = 0
	}
	if offset > total {
		offset = total
	}
	end := offset + limit
	if limit <= 0 || end > total {
		end = total
	}
	out := make([]*Task, 0, end-offset)
	for _, t := range ts[offset:end] {
		out = append(out, t.Snapshot())
	}
	return out, total
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
	if t.Status != StatusRunning && t.Status != StatusDispatching {
		t.mu.Unlock()
		return
	}
	if percent >= 0 {
		t.Percent = percent
	}
	if msg != "" {
		t.Message = msg
	}
	t.mu.Unlock()
	m.persist(t)
}

// OnResult 处理执行结果：非终态任务收敛到 success/failed。
func (m *Manager) OnResult(nodeID, taskID, status, errMsg string, result json.RawMessage) {
	t := m.lookup(nodeID, taskID)
	if t == nil {
		return
	}
	t.mu.Lock()
	if t.Status.Terminal() {
		t.mu.Unlock()
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
	t.mu.Unlock()
	m.persist(t)
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
	m.persist(t)

	if sender, ok := m.router.SenderFor(nodeID); ok {
		env, _ := agentprotocol.New(agentprotocol.TypeTaskCancel, "", agentprotocol.TaskCancelPayload{
			TaskID: taskID, Reason: reason,
		})
		sender.Send(env)
	}
	return nil
}

// AdminCancel 管理端取消：按任务 ID 取消，节点取自任务本身（无归属校验）。
func (m *Manager) AdminCancel(taskID, reason string) error {
	m.mu.RLock()
	t, ok := m.tasks[taskID]
	m.mu.RUnlock()
	if !ok {
		return ErrUnknownTask
	}
	return m.Cancel(t.NodeID, taskID, reason)
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
			m.persist(t)
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
	if t.Status != from {
		t.mu.Unlock()
		return
	}
	if errMsg != "" {
		t.Error = errMsg
	}
	t.setStatus(to)
	t.mu.Unlock()
	m.persist(t)
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
