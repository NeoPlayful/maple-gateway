package enrollment

import (
	"context"
	"errors"
	"fmt"

	"github.com/NeoPlayful/maple-gateway/server/internal/containermanager/agentconn"
	"github.com/NeoPlayful/maple-gateway/server/internal/containermanager/nodes"
)

// Registrar 解析节点身份：把节点名解析为 Gateway 节点 UUID（get-or-create），
// 并刷新其心跳。由 CM 侧对 gwclient.GatewayClient 的薄适配实现。
type Registrar interface {
	// ResolveNode 按 name 取得 Gateway 节点 UUID；不存在则创建。返回 UUID 字符串。
	ResolveNode(ctx context.Context, name, host, region string, labels map[string]string) (string, error)
	// HeartbeatNode 刷新 Gateway 节点心跳（避免被 Gateway watchdog 判 offline）。
	HeartbeatNode(ctx context.Context, nodeID string) error
}

// Manager 组合一次性 Token、节点身份解析（Gateway）与统一节点运行期模型。
//
// 首注册时先经 Registrar 把节点名解析为 Gateway UUID，再在 nodes.Store 建立
// 运行期记录并发放凭证；重连时凭 node_id(=Gateway UUID) + 凭证放行。
type Manager struct {
	tokens    *TokenStore
	nodes     *nodes.Store
	reg       Registrar
	requireEn bool // 是否强制首注册必须携带有效 Enrollment Token
}

// NewManager 构造准入管理器（Token 存储退化为进程内）。
func NewManager(requireEnrollment bool, nodeStore *nodes.Store, reg Registrar) *Manager {
	return NewManagerWithTokens(requireEnrollment, nodeStore, reg, NewTokenStore())
}

// NewManagerWithTokens 构造准入管理器，使用外部提供的 Token 存储（可带持久化）。
func NewManagerWithTokens(requireEnrollment bool, nodeStore *nodes.Store, reg Registrar, tokens *TokenStore) *Manager {
	if tokens == nil {
		tokens = NewTokenStore()
	}
	return &Manager{tokens: tokens, nodes: nodeStore, reg: reg, requireEn: requireEnrollment}
}

// Tokens 暴露 Token 存储，供管理端签发/撤销。
func (m *Manager) Tokens() *TokenStore { return m.tokens }

// Nodes 暴露统一节点运行期存储，供状态查询/凭证吊销。
func (m *Manager) Nodes() *nodes.Store { return m.nodes }

// Authenticate 实现 agentconn.Authenticator：
//   - 携带 node_id + 凭证：重连路径，校验凭证后刷新心跳；
//   - 首次注册：经 Registrar 解析/创建 Gateway 节点，建立运行期记录并发放凭证；
//   - requireEnrollment 时首注册必须携带有效 Enrollment Token。
func (m *Manager) Authenticate(ctx context.Context, hello agentconn.Hello) (agentconn.AuthResult, error) {
	// 重连：已有 node_id（Gateway UUID）。
	if hello.NodeID != "" {
		if r, ok := m.nodes.Get(hello.NodeID); ok && !r.Revoked {
			if m.requireEn {
				if err := m.nodes.VerifyCredential(hello.NodeID, hello.Credential); err != nil {
					return agentconn.AuthResult{}, err
				}
			}
			m.nodes.UpdateHello(hello.NodeID, hello.OS, hello.Arch, hello.AgentVersion)
			if m.reg != nil {
				_ = m.reg.HeartbeatNode(ctx, hello.NodeID)
			}
			return agentconn.AuthResult{NodeID: hello.NodeID}, nil
		}
		// node_id 未知或已吊销：不静默放行，交首次注册流程重新认领。
		if m.requireEn {
			return agentconn.AuthResult{}, fmt.Errorf("%w: node %s not registered", nodes.ErrUnknown, hello.NodeID)
		}
	}

	// 首次注册（或宽松模式下重新认领）。
	return m.enroll(ctx, hello)
}

// enroll 处理首次注册：校验 Token → 解析 Gateway 节点 → 建立运行期记录 → 发放凭证。
func (m *Manager) enroll(ctx context.Context, hello agentconn.Hello) (agentconn.AuthResult, error) {
	if m.requireEn && hello.EnrollmentToken == "" {
		return agentconn.AuthResult{}, errors.New("enrollment token required")
	}
	if m.requireEn {
		if err := m.tokens.Peek(hello.EnrollmentToken); err != nil {
			return agentconn.AuthResult{}, err
		}
	}
	if m.reg == nil {
		return agentconn.AuthResult{}, errors.New("node registrar not configured")
	}
	name := hello.Hostname
	if name == "" {
		name = hello.NodeID
	}
	if name == "" {
		return agentconn.AuthResult{}, errors.New("hello missing node name")
	}
	uuid, err := m.reg.ResolveNode(ctx, name, hello.RemoteHost, "", nil)
	if err != nil {
		return agentconn.AuthResult{}, err
	}
	cred, err := m.nodes.Ensure(ctx, uuid, name, hello.OS, hello.Arch, hello.AgentVersion)
	if err != nil {
		return agentconn.AuthResult{}, err
	}
	if m.requireEn {
		if err := m.tokens.Consume(hello.EnrollmentToken, uuid); err != nil {
			return agentconn.AuthResult{}, err
		}
	}
	return agentconn.AuthResult{NodeID: uuid, Credential: cred, NeedCredential: true}, nil
}

// Verify 实现 agentconn.Authenticator：校验节点上行时携带的凭证。
// 宽松模式下（未强制注册）恒放行。
func (m *Manager) Verify(nodeID, credential string) error {
	if !m.requireEn {
		return nil
	}
	return m.nodes.VerifyCredential(nodeID, credential)
}
