// Package agentregistry 维护 CM 视野内的节点与其 Node Agent 客户端。
//
// 节点来源：CM 配置静态登记（本期）；每个节点持有一个指向其 Agent 的 HTTP 客户端。
// CM 周期探测 Agent（/health、/node/info）判断节点可用性，并据此采集容器实际态。
package agentregistry

import (
	"sync"

	"github.com/NeoPlayful/maple-gateway/server/internal/containermanager/config"
	"github.com/NeoPlayful/maple-gateway/server/internal/containermanager/gwclient"
)

// Node 是 CM 视野内的一个节点。
type Node struct {
	Name       string
	Host       string
	Region     string
	Labels     map[string]string
	Agent      *gwclient.AgentClient // 指向该节点 Agent 的客户端
	Healthy    bool                  // 最近一次探测是否可达
	GatewayID  string                // 上报 Gateway 后回填的节点 ID（用于心跳）
	LastSeenMs int64                 // 最近一次探测时间（Unix 毫秒）
}

// Registry 是节点注册表。
type Registry struct {
	mu    sync.RWMutex
	nodes map[string]*Node // key: 节点名
	order []string         // 保持配置顺序，便于稳定遍历
}

// New 从配置构建注册表。
func New(nodes []config.NodeConfig) *Registry {
	r := &Registry{nodes: make(map[string]*Node, len(nodes))}
	for _, n := range nodes {
		if n.Name == "" || n.AgentAddr == "" {
			continue
		}
		r.nodes[n.Name] = &Node{
			Name:   n.Name,
			Host:   n.Host,
			Region: n.Region,
			Labels: n.Labels,
			Agent:  gwclient.NewAgentClient(n.AgentAddr, n.AgentToken),
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

// SetHealth 更新节点可用性与其上报 Gateway 后回填的 ID。
func (r *Registry) SetHealth(name string, healthy bool, gatewayID string, seenMs int64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if n, ok := r.nodes[name]; ok {
		n.Healthy = healthy
		n.LastSeenMs = seenMs
		if gatewayID != "" {
			n.GatewayID = gatewayID
		}
	}
}
