// Package nodestate 维护节点的在线状态机。
//
// 依据最近一次心跳/会话活动时间，按固定阈值推进状态：
//
//	0 ~ 30s   Online    正常
//	30 ~ 90s  Unstable  心跳丢失，可能抖动
//	> 90s     Offline   判定离线
//
// 会话建立/心跳到达即刷新活跃时间；会话断开立即进入离线判定起点。
package nodestate

import (
	"sync"
	"time"
)

// State 是节点在线状态。
type State string

const (
	Online   State = "online"
	Unstable State = "unstable"
	Offline  State = "offline"
)

// 默认阈值。
const (
	DefaultUnstableAfter = 30 * time.Second
	DefaultOfflineAfter  = 90 * time.Second
)

// entry 是单节点的状态记录。
type entry struct {
	state     State
	lastSeen  time.Time
	connected bool
}

// Tracker 是节点在线状态机。
type Tracker struct {
	mu            sync.RWMutex
	entries       map[string]*entry
	unstableAfter time.Duration
	offlineAfter  time.Duration
}

// New 构造状态机。阈值 <= 0 时取默认值。
func New(unstableAfter, offlineAfter time.Duration) *Tracker {
	if unstableAfter <= 0 {
		unstableAfter = DefaultUnstableAfter
	}
	if offlineAfter <= 0 {
		offlineAfter = DefaultOfflineAfter
	}
	return &Tracker{
		entries:       make(map[string]*entry),
		unstableAfter: unstableAfter,
		offlineAfter:  offlineAfter,
	}
}

// Touch 记录一次活跃（会话建立或心跳到达），状态置 Online。
func (t *Tracker) Touch(nodeID string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	e := t.entries[nodeID]
	if e == nil {
		e = &entry{}
		t.entries[nodeID] = e
	}
	e.lastSeen = time.Now()
	e.state = Online
	e.connected = true
}

// Disconnect 标记会话断开：保留 lastSeen，由后续 Evaluate 推进到 Unstable/Offline。
func (t *Tracker) Disconnect(nodeID string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if e, ok := t.entries[nodeID]; ok {
		e.connected = false
	}
}

// State 返回节点当前状态；未知节点视为 Offline。
func (t *Tracker) State(nodeID string) State {
	t.mu.RLock()
	defer t.mu.RUnlock()
	if e, ok := t.entries[nodeID]; ok {
		return e.state
	}
	return Offline
}

// LastSeen 返回最近活跃时间；未知节点返回零值。
func (t *Tracker) LastSeen(nodeID string) time.Time {
	t.mu.RLock()
	defer t.mu.RUnlock()
	if e, ok := t.entries[nodeID]; ok {
		return e.lastSeen
	}
	return time.Time{}
}

// Evaluate 依据当前时间推进所有节点的状态，返回状态发生变化的节点。
func (t *Tracker) Evaluate() map[string]State {
	now := time.Now()
	changed := make(map[string]State)
	t.mu.Lock()
	defer t.mu.Unlock()
	for id, e := range t.entries {
		var want State
		elapsed := now.Sub(e.lastSeen)
		switch {
		case elapsed >= t.offlineAfter:
			want = Offline
		case elapsed >= t.unstableAfter:
			want = Unstable
		default:
			want = Online
		}
		if want != e.state {
			e.state = want
			changed[id] = want
		}
	}
	return changed
}

// Snapshot 返回全部节点的状态与最近活跃时间副本。
func (t *Tracker) Snapshot() map[string]NodeState {
	t.mu.RLock()
	defer t.mu.RUnlock()
	out := make(map[string]NodeState, len(t.entries))
	for id, e := range t.entries {
		out[id] = NodeState{State: e.state, LastSeen: e.lastSeen, Connected: e.connected}
	}
	return out
}

// NodeState 是节点的对外状态视图。
type NodeState struct {
	State     State
	LastSeen  time.Time
	Connected bool
}
