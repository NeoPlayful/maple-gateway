package agentconn

import (
	"sync"

	"github.com/NeoPlayful/maple-gateway/server/internal/containermanager/tasksys"
)

// Hub 是活跃 Agent 会话注册表，保证单节点单活跃会话。
// 同节点新会话注册时旧会话被顶替（kicked），避免旧连接残留造成命令双发。
type Hub struct {
	mu       sync.RWMutex
	sessions map[string]*Session // key: node_id
}

// NewHub 构造空会话注册表。
func NewHub() *Hub {
	return &Hub{sessions: make(map[string]*Session)}
}

// Add 注册会话；若该节点已有会话则返回旧会话供调用方关闭（kicked）。
func (h *Hub) Add(s *Session) (kicked *Session) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if old, ok := h.sessions[s.NodeID]; ok {
		kicked = old
	}
	h.sessions[s.NodeID] = s
	return kicked
}

// Get 取指定节点的活跃会话。
func (h *Hub) Get(nodeID string) (*Session, bool) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	s, ok := h.sessions[nodeID]
	return s, ok
}

// Remove 摘除会话，且仅当注册表内当前会话确实是 s 时才移除：
// 避免被顶替的旧会话在延迟退出时误删新会话。
func (h *Hub) Remove(s *Session) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if cur, ok := h.sessions[s.NodeID]; ok && cur == s {
		delete(h.sessions, s.NodeID)
	}
}

// Online 返回当前有活跃会话的节点 ID 列表。
func (h *Hub) Online() []string {
	h.mu.RLock()
	defer h.mu.RUnlock()
	out := make([]string, 0, len(h.sessions))
	for id := range h.sessions {
		out = append(out, id)
	}
	return out
}

// IsOnline 报告指定节点是否有活跃会话。
func (h *Hub) IsOnline(nodeID string) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	_, ok := h.sessions[nodeID]
	return ok
}

// SenderFor 实现 tasksys.Router：按节点 ID 返回活跃会话作为发送通道。
func (h *Hub) SenderFor(nodeID string) (tasksys.Sender, bool) {
	s, ok := h.Get(nodeID)
	if !ok {
		return nil, false
	}
	return s, true
}
