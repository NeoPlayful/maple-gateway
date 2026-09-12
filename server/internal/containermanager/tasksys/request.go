package tasksys

import "github.com/NeoPlayful/maple-gateway/server/internal/agentprotocol"

// Sender 抽象「把一条消息投递到某节点」的能力，由 agentconn 会话实现。
// 返回 false 表示投递失败（节点不在线或连接已失效）。
type Sender interface {
	Send(env agentprotocol.Envelope) bool
}

// SenderFunc 便于把闭包适配为 Sender。
type SenderFunc func(env agentprotocol.Envelope) bool

// Send 实现 Sender。
func (f SenderFunc) Send(env agentprotocol.Envelope) bool { return f(env) }

// Router 按节点 ID 查找活跃发送通道。
type Router interface {
	SenderFor(nodeID string) (Sender, bool)
}

// RouterFunc 便于把闭包适配为 Router。
type RouterFunc func(nodeID string) (Sender, bool)

// SenderFor 实现 Router。
func (f RouterFunc) SenderFor(nodeID string) (Sender, bool) { return f(nodeID) }
