package enrollment

import (
	"errors"
	"fmt"

	"github.com/NeoPlayful/maple-gateway/server/internal/containermanager/agentconn"
)

// Manager 组合 Token 与节点存储，向 agentconn 提供准入判定。
// 未启用 Token 时（tokens 为空且令牌校验关闭）走宽松模式：任一 agent.hello 均放行，
// 便于本地开发；生产应配置 enrollment 强制令牌。
type Manager struct {
	tokens    *TokenStore
	nodes     *NodeStore
	requireEn bool // 是否强制首注册必须携带有效 Enrollment Token
}

// NewManager 构造准入管理器。requireEnrollment=true 时首注册必须携带有效 Token。
func NewManager(requireEnrollment bool) *Manager {
	return &Manager{
		tokens:    NewTokenStore(),
		nodes:     NewNodeStore(),
		requireEn: requireEnrollment,
	}
}

// Tokens 暴露 Token 存储，供管理端签发/撤销。
func (m *Manager) Tokens() *TokenStore { return m.tokens }

// Nodes 暴露节点存储，供状态查询/凭证吊销。
func (m *Manager) Nodes() *NodeStore { return m.nodes }

// Authenticate 实现 agentconn.Authenticator：
//   - 携带 enrollment_token：首注册路径，消费 Token 并创建节点、发放凭证；
//   - 携带 node_id + 凭证：重连路径，校验凭证后返回既有节点；
//   - 其余情况：按配置决定放行（宽松）或拒绝。
func (m *Manager) Authenticate(hello agentconn.Hello) (agentconn.AuthResult, error) {
	switch {
	case hello.EnrollmentToken != "":
		return m.enroll(hello)
	case hello.NodeID != "":
		// 重连：强制注册时校验节点凭证。
		if n, ok := m.nodes.Get(hello.NodeID); ok && !n.Revoked {
			if m.requireEn {
				if err := m.nodes.VerifyCredential(hello.NodeID, hello.Credential); err != nil {
					return agentconn.AuthResult{}, err
				}
			}
			m.nodes.UpdateHello(hello.NodeID, hello.Hostname, hello.OS, hello.Arch, hello.AgentVersion)
			return agentconn.AuthResult{NodeID: hello.NodeID}, nil
		}
		if m.requireEn {
			return agentconn.AuthResult{}, fmt.Errorf("%w: node %s not registered", ErrNodeUnknown, hello.NodeID)
		}
		// 宽松模式：为未知 node_id 补建节点（开发环境）。
		n, err := m.nodes.Add(&Node{
			ID: hello.NodeID, Hostname: hello.Hostname, OS: hello.OS,
			Arch: hello.Arch, AgentVersion: hello.AgentVersion,
		})
		if err != nil {
			return agentconn.AuthResult{}, err
		}
		return agentconn.AuthResult{
			NodeID:         n.ID,
			Credential:     n.Credential,
			NeedCredential: true,
		}, nil
	default:
		if m.requireEn {
			return agentconn.AuthResult{}, errors.New("enrollment token required")
		}
		// 宽松模式且无任何标识：拒绝（无法定位节点）。
		return agentconn.AuthResult{}, errors.New("hello missing node_id and enrollment_token")
	}
}

// enroll 处理首注册：消费 Token → 创建节点 → 发放凭证。
func (m *Manager) enroll(hello agentconn.Hello) (agentconn.AuthResult, error) {
	if m.requireEn {
		if err := m.tokens.Peek(hello.EnrollmentToken); err != nil {
			return agentconn.AuthResult{}, err
		}
	}
	n, err := m.nodes.Add(&Node{
		Name:         hello.Hostname,
		Hostname:     hello.Hostname,
		OS:           hello.OS,
		Arch:         hello.Arch,
		AgentVersion: hello.AgentVersion,
	})
	if err != nil {
		return agentconn.AuthResult{}, err
	}
	if m.requireEn {
		if err := m.tokens.Consume(hello.EnrollmentToken, n.ID); err != nil {
			return agentconn.AuthResult{}, err
		}
	}
	return agentconn.AuthResult{NodeID: n.ID, Credential: n.Credential, NeedCredential: true}, nil
}

// Verify 实现 agentconn.Authenticator：校验重连节点上行时的凭证。
// 宽松模式下（未强制注册）恒放行。
func (m *Manager) Verify(nodeID, credential string) error {
	if !m.requireEn {
		return nil
	}
	return m.nodes.VerifyCredential(nodeID, credential)
}
