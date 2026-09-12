package wsclient

import (
	"context"
	"time"

	"github.com/NeoPlayful/maple-gateway/server/internal/agentprotocol"
	"github.com/gorilla/websocket"
)

// WSDialer 是基于 gorilla/websocket 的 Dialer 实现。
type WSDialer struct{}

// NewWSDialer 构造 WebSocket 拨号器。
func NewWSDialer() *WSDialer { return &WSDialer{} }

// Dial 建立到 CM 的 WebSocket 连接。
func (WSDialer) Dial(ctx context.Context, url string) (Conn, error) {
	d := websocket.Dialer{
		HandshakeTimeout: dialTimeout,
		ReadBufferSize:   4096,
		WriteBufferSize:  4096,
	}
	conn, _, err := d.DialContext(ctx, url, nil)
	if err != nil {
		return nil, err
	}
	conn.SetReadLimit(maxMessageSize)
	return &wsConn{conn: conn}, nil
}

// wsConn 把 gorilla 连接适配为 Conn。
type wsConn struct {
	conn *websocket.Conn
}

// WriteEnvelope 发送一条消息。
func (c *wsConn) WriteEnvelope(env agentprotocol.Envelope) error {
	return c.conn.WriteJSON(env)
}

// ReadEnvelope 读取下一条消息；收到 ping 自动回 pong。
func (c *wsConn) ReadEnvelope() (agentprotocol.Envelope, error) {
	for {
		var env agentprotocol.Envelope
		if err := c.conn.ReadJSON(&env); err != nil {
			return agentprotocol.Envelope{}, err
		}
		return env, nil
	}
}

// SetReadDeadline 设置读超时。
func (c *wsConn) SetReadDeadline(t time.Time) error { return c.conn.SetReadDeadline(t) }

// SetWriteDeadline 设置写超时。
func (c *wsConn) SetWriteDeadline(t time.Time) error { return c.conn.SetWriteDeadline(t) }

// Close 关闭连接。
func (c *wsConn) Close() error { return c.conn.Close() }
