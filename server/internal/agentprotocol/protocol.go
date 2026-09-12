// Package agentprotocol 定义 Container Manager 与 Container Agent 之间
// 单条持久 WebSocket 上的统一消息契约，供双方共用。
//
// 方向约定：Agent 主动连 CM（反连），节点不开放入站端口。连接建立后，
// 命令下行与状态上行复用同一条连接：
//
//	连接类  agent.hello → agent.ready → heartbeat（周期）
//	状态类  node.info / node.metrics / docker.event / container.snapshot
//	任务类  task.execute（下行）→ task.ack / task.progress / task.result（上行）
//	日志类  logs.open / logs.data / logs.close
//
// 所有消息统一用 Envelope 包裹；payload 为目标类型的 JSON。
package agentprotocol

import (
	"encoding/json"
	"time"
)

// MessageType 是消息类型标识（Envelope.Type）。
type MessageType string

const (
	// 连接类。
	TypeAgentHello MessageType = "agent.hello" // Agent → CM：握手（首次带 enrollment token）
	TypeAgentReady MessageType = "agent.ready" // CM → Agent：握手完成，回发凭证与心跳参数
	TypeHeartbeat  MessageType = "heartbeat"   // Agent → CM：存活心跳

	// 状态类。
	TypeNodeInfo        MessageType = "node.info"         // Agent → CM：节点静态信息（主机/内核/Docker）
	TypeNodeMetrics     MessageType = "node.metrics"      // Agent → CM：主机资源指标
	TypeDockerEvent     MessageType = "docker.event"      // Agent → CM：Docker 事件
	TypeContainerSnap   MessageType = "container.snapshot" // Agent → CM：受管容器快照（兜底/周期）
	TypeNodeStatusEvent MessageType = "node.status"       // CM → Agent：节点状态变化通知（可选）

	// 任务类。
	TypeTaskExecute  MessageType = "task.execute"  // CM → Agent：下发任务
	TypeTaskAck      MessageType = "task.ack"      // Agent → CM：已接受
	TypeTaskProgress MessageType = "task.progress" // Agent → CM：执行进度
	TypeTaskResult   MessageType = "task.result"   // Agent → CM：执行结果
	TypeTaskCancel   MessageType = "task.cancel"   // CM → Agent：取消

	// 日志类。
	TypeLogsOpen  MessageType = "logs.open"  // CM → Agent：打开日志流
	TypeLogsData  MessageType = "logs.data"  // Agent → CM：日志分片
	TypeLogsClose MessageType = "logs.close" // 双向：关闭日志流
)

// Envelope 是所有消息的统一包裹。
type Envelope struct {
	Type      MessageType     `json:"type"`
	RequestID string          `json:"request_id,omitempty"`
	Timestamp int64           `json:"timestamp"` // Unix 毫秒
	Payload   json.RawMessage `json:"payload,omitempty"`
}

// Now 返回当前 Unix 毫秒时间戳（填充 Envelope.Timestamp 用）。
func Now() int64 { return time.Now().UnixMilli() }

// New 构造一条消息；payload 为 nil 时省略 payload 字段。
func New(t MessageType, requestID string, payload any) (Envelope, error) {
	env := Envelope{Type: t, RequestID: requestID, Timestamp: Now()}
	if payload != nil {
		raw, err := json.Marshal(payload)
		if err != nil {
			return Envelope{}, err
		}
		env.Payload = raw
	}
	return env, nil
}

// DecodePayload 把 Envelope.Payload 解码到 out。
func (e Envelope) DecodePayload(out any) error {
	if len(e.Payload) == 0 {
		return nil
	}
	return json.Unmarshal(e.Payload, out)
}
