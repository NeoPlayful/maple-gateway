// Package agentconn 承载 Container Manager 侧的 Agent 接入：
// 作为 WebSocket 服务端接收 Agent 主动反连，维护单节点单活跃会话，
// 并提供向节点下发消息（任务/日志指令）与会话生命周期回调。
//
// 节点自身不开放任何管理端口，全部命令经这条持久连接下行。
package agentconn

import (
	"sync"
	"time"

	"github.com/NeoPlayful/maple-gateway/server/internal/agentprotocol"
	"github.com/gorilla/websocket"
	"go.uber.org/zap"
)

const (
	// writeWait 单条消息写超时。
	writeWait = 10 * time.Second
	// pongWait 读空闲上限：超过则判定连接失效。取心跳周期的数倍以容忍抖动。
	pongWait = 90 * time.Second
	// pingPeriod 服务端主动探活周期（须小于 pongWait）。
	pingPeriod = 30 * time.Second
	// maxMessageSize 单条上行消息大小上限。
	maxMessageSize = 1 << 20
	// sendBufSize 每会话下行发送队列长度；队列写满即视为慢连接。
	sendBufSize = 64
	// handshakeWait 等待 agent.hello 的时限。
	handshakeWait = 15 * time.Second
	// defaultHeartbeatSec 未指定时下发给 Agent 的心跳周期。
	defaultHeartbeatSec = 15
)

// Session 是一条已建立的 Agent 连接。
type Session struct {
	NodeID string

	conn   *websocket.Conn
	send   chan agentprotocol.Envelope
	logger *zap.Logger

	closeOnce sync.Once
	done      chan struct{}
	kicked    bool // 是否被同节点的新会话顶替
}

func newSession(nodeID string, conn *websocket.Conn, logger *zap.Logger) *Session {
	return &Session{
		NodeID: nodeID,
		conn:   conn,
		send:   make(chan agentprotocol.Envelope, sendBufSize),
		logger: logger,
		done:   make(chan struct{}),
	}
}

// Send 把消息投入写队列。会话已关闭或队列写满时返回 false，
// 后者代表该连接发送能力跟不上，交上层决定是否断开。
func (s *Session) Send(env agentprotocol.Envelope) bool {
	select {
	case <-s.done:
		return false
	default:
	}
	select {
	case s.send <- env:
		return true
	case <-s.done:
		return false
	default:
		return false
	}
}

// Kicked 报告本会话是否被同节点的新会话顶替。
func (s *Session) Kicked() bool {
	select {
	case <-s.done:
		return s.kicked
	default:
		return false
	}
}

// close 关闭会话（幂等）。kicked 标记其被新会话顶替。
func (s *Session) close(kicked bool) {
	s.closeOnce.Do(func() {
		s.kicked = kicked
		close(s.done)
		_ = s.conn.Close()
	})
}

// writePump 串行化下行写：把队列消息与周期 ping 写入连接。
// 连接关闭或写失败时退出并关闭会话。
func (s *Session) writePump() {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		s.close(false)
	}()
	for {
		select {
		case env := <-s.send:
			_ = s.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := s.conn.WriteJSON(env); err != nil {
				s.logger.Debug("agent write failed", zap.String("node_id", s.NodeID), zap.Error(err))
				return
			}
		case <-ticker.C:
			_ = s.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := s.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		case <-s.done:
			return
		}
	}
}
