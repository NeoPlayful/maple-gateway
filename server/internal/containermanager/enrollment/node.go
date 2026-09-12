package enrollment

import (
	"crypto/subtle"
	"errors"
	"sync"
	"time"
)

// ErrNodeUnknown 表示节点不存在或凭证不匹配。
var ErrNodeUnknown = errors.New("node unknown or credential mismatch")

// Node 是 CM 登记的一个已注册节点。
type Node struct {
	ID           string            `json:"id"`
	Name         string            `json:"name"`
	Hostname     string            `json:"hostname,omitempty"`
	Host         string            `json:"host,omitempty"`
	Region       string            `json:"region,omitempty"`
	Labels       map[string]string `json:"labels,omitempty"`
	AgentVersion string            `json:"agent_version,omitempty"`
	OS           string            `json:"os,omitempty"`
	Arch         string            `json:"arch,omitempty"`
	Credential   string            `json:"-"`
	RegisteredMs int64             `json:"registered_at_ms"`
	Revoked      bool              `json:"revoked"`
}

// NodeStore 是已注册节点的内存存储。
type NodeStore struct {
	mu    sync.RWMutex
	nodes map[string]*Node // key: node_id
}

// NewNodeStore 构造空节点存储。
func NewNodeStore() *NodeStore {
	return &NodeStore{nodes: make(map[string]*Node)}
}

// Add 登记一个新节点并为它生成独立凭证。
func (s *NodeStore) Add(n *Node) (*Node, error) {
	cred, err := randomToken("mg_node_")
	if err != nil {
		return nil, err
	}
	if n.ID == "" {
		id, err := randomToken("node_")
		if err != nil {
			return nil, err
		}
		n.ID = id
	}
	n.Credential = cred
	n.RegisteredMs = time.Now().UnixMilli()
	s.mu.Lock()
	s.nodes[n.ID] = n
	s.mu.Unlock()
	return n, nil
}

// Get 按 ID 取节点。
func (s *NodeStore) Get(id string) (*Node, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	n, ok := s.nodes[id]
	return n, ok
}

// List 返回全部节点（顺序不定，由调用方排序）。
func (s *NodeStore) List() []*Node {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*Node, 0, len(s.nodes))
	for _, n := range s.nodes {
		out = append(out, n)
	}
	return out
}

// VerifyCredential 校验节点凭证（常量时间比较，避免时序侧信道）。
func (s *NodeStore) VerifyCredential(id, cred string) error {
	s.mu.RLock()
	n, ok := s.nodes[id]
	s.mu.RUnlock()
	if !ok || n.Revoked {
		return ErrNodeUnknown
	}
	if subtle.ConstantTimeCompare([]byte(n.Credential), []byte(cred)) != 1 {
		return ErrNodeUnknown
	}
	return nil
}

// Revoke 吊销节点凭证（节点仍保留在册，标记为已吊销）。
func (s *NodeStore) Revoke(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	n, ok := s.nodes[id]
	if !ok {
		return false
	}
	n.Revoked = true
	n.Credential = ""
	return true
}

// UpdateHello 用最近一次握手信息刷新节点静态字段。
func (s *NodeStore) UpdateHello(id, hostname, osName, arch, agentVersion string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	n, ok := s.nodes[id]
	if !ok {
		return
	}
	if hostname != "" {
		n.Hostname = hostname
	}
	if osName != "" {
		n.OS = osName
	}
	if arch != "" {
		n.Arch = arch
	}
	if agentVersion != "" {
		n.AgentVersion = agentVersion
	}
}
