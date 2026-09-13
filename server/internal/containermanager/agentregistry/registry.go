// Package agentregistry 维护 CM 视野内的节点视图与向节点下发命令的通道。
//
// 节点身份只有两处事实，且不重复：
//   - Gateway 的 nodes 表：name/host/region/labels（本包作为视图缓存）。
//   - nodes.Store：运行期（凭证、OS/arch、在线状态）。
//
// 二者以 Node.ID（Gateway 节点 UUID）关联，本包不另存在线状态，只经 nodes.Store 读取。
// 命令下发统一走 Commander（WS 任务通道），不再有指向 Agent 的 HTTP 客户端。
package agentregistry

import (
	"context"
	"encoding/json"
	"sync"

	"github.com/NeoPlayful/maple-gateway/server/internal/containermanager/config"
	"github.com/NeoPlayful/maple-gateway/server/internal/containermanager/nodes"
)

// Commander 是向某节点下发一次命令并等待结果的通道（由 tasksys.Manager 实现）。
type Commander interface {
	Call(ctx context.Context, nodeID, action string, params any) (json.RawMessage, error)
}

// Node 是 CM 视野内的一个节点。
type Node struct {
	Name   string
	Host   string
	Region string
	Labels map[string]string

	states *nodes.Store
	cmd    Commander

	// idMu 保护 id：观测循环写入、管理端/控制通道并发读取。
	idMu sync.RWMutex
	id   string // Gateway 节点 UUID（= nodes.Store 键）；未注册前为空
}

// ID 返回节点所属的 Gateway 节点 UUID（未注册前为空）。
func (n *Node) ID() string {
	n.idMu.RLock()
	defer n.idMu.RUnlock()
	return n.id
}

// SetID 绑定节点所属的 Gateway 节点 UUID。
func (n *Node) SetID(id string) {
	n.idMu.Lock()
	n.id = id
	n.idMu.Unlock()
}

// Online 报告节点当前是否有活跃 WS 会话（在线状态唯一来源：nodes.Store）。
func (n *Node) Online() bool {
	id := n.ID()
	if n.states == nil || id == "" {
		return false
	}
	return n.states.Online(id)
}

// Call 经 WS 任务通道向本节点下发一次命令并等待结果。
func (n *Node) Call(ctx context.Context, action string, params any) (json.RawMessage, error) {
	return n.cmd.Call(ctx, n.ID(), action, params)
}

// Registry 是节点视图。
type Registry struct {
	mu    sync.RWMutex
	nodes map[string]*Node // key: 节点名
	order []string

	states *nodes.Store
	cmd    Commander
}

// New 从配置构建节点视图。cmd 为命令下发通道；states 为在线状态来源（可空）。
func New(cfgNodes []config.NodeConfig, cmd Commander, states *nodes.Store) *Registry {
	r := &Registry{
		nodes:  make(map[string]*Node, len(cfgNodes)),
		states: states,
		cmd:    cmd,
	}
	for _, n := range cfgNodes {
		if n.Name == "" {
			continue
		}
		r.nodes[n.Name] = &Node{
			Name:   n.Name,
			Host:   n.Host,
			Region: n.Region,
			Labels: n.Labels,
			states: states,
			cmd:    cmd,
		}
		r.order = append(r.order, n.Name)
	}
	return r
}

// All 按配置顺序返回全部节点。
func (r *Registry) All() []*Node {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]*Node, 0, len(r.order))
	for _, name := range r.order {
		if n, ok := r.nodes[name]; ok {
			out = append(out, n)
		}
	}
	return out
}

// Get 按名取节点。
func (r *Registry) Get(name string) (*Node, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	n, ok := r.nodes[name]
	return n, ok
}

// GetByID 按 Gateway 节点 UUID 取节点。
func (r *Registry) GetByID(id string) (*Node, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, n := range r.nodes {
		if n.ID() == id {
			return n, true
		}
	}
	return nil, false
}

// Bind 把节点名绑定到 Gateway 节点 UUID（Agent 注册时调用）。未登记的节点名忽略。
func (r *Registry) Bind(name, nodeID string) {
	r.mu.RLock()
	n, ok := r.nodes[name]
	r.mu.RUnlock()
	if ok {
		n.SetID(nodeID)
	}
}
