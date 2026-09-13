// Package logstream 承载 CM 侧的容器日志流多路复用。
//
// 一条流由 CM 生成唯一 stream_id，经 logs.open 下发到 Agent；Agent 随日志产出
// 上行 logs.data（携带同一 stream_id），CM 据此路由回对应订阅者。
// 每条流是有界通道：消费端（Gateway 代理）跟不上时丢弃分片而非阻塞 Agent 上行，
// 由 UI 的"日志可能不完整"提示兜底。
package logstream

import (
	"sync"

	"github.com/google/uuid"
)

// chunkBuf 是单条流的缓冲分片数；消费端落后超过该值即开始丢弃。
const chunkBuf = 256

// Stream 是一条打开的日志流（一次订阅）。
type Stream struct {
	ID         string
	InstanceID string

	mu     sync.Mutex
	ch     chan []byte
	done   chan struct{}
	closed bool
	// overflow 记录曾因消费端落后而丢弃分片，供 UI 提示日志不完整。
	overflow bool
}

// Chunks 返回只读分片通道；关闭时通道被关闭。
func (s *Stream) Chunks() <-chan []byte { return s.ch }

// Done 返回关闭信号通道。
func (s *Stream) Done() <-chan struct{} { return s.done }

// Overflow 报告该流是否发生过分片丢弃。
func (s *Stream) Overflow() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.overflow
}

// deliver 投递一片日志；通道满则丢弃并标记 overflow（不阻塞上行）。
func (s *Stream) deliver(data []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	select {
	case s.ch <- data:
	default:
		s.overflow = true
	}
}

// Close 关闭流（幂等）：关闭分片通道与 done 信号。
func (s *Stream) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	s.closed = true
	close(s.done)
	close(s.ch)
}

// Hub 是活跃日志流的注册表（stream_id → Stream）。
type Hub struct {
	mu      sync.RWMutex
	streams map[string]*Stream
}

// NewHub 构造空注册表。
func NewHub() *Hub { return &Hub{streams: make(map[string]*Stream)} }

// Open 为某实例打开一条新流并登记。
func (h *Hub) Open(instanceID string) *Stream {
	s := &Stream{
		ID:         "stream_" + uuid.NewString(),
		InstanceID: instanceID,
		ch:         make(chan []byte, chunkBuf),
		done:       make(chan struct{}),
	}
	h.mu.Lock()
	h.streams[s.ID] = s
	h.mu.Unlock()
	return s
}

// Get 按 stream_id 取流。
func (h *Hub) Get(streamID string) (*Stream, bool) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	s, ok := h.streams[streamID]
	return s, ok
}

// Dispatch 把一片上行日志路由到所属流；eof 时关闭该流。
func (h *Hub) Dispatch(streamID, data string, eof bool) {
	s, ok := h.Get(streamID)
	if !ok {
		return
	}
	if data != "" {
		s.deliver([]byte(data))
	}
	if eof {
		h.Close(streamID)
	}
}

// Close 关闭并从注册表移除某流（幂等）。
func (h *Hub) Close(streamID string) {
	h.mu.Lock()
	s, ok := h.streams[streamID]
	if ok {
		delete(h.streams, streamID)
	}
	h.mu.Unlock()
	if ok {
		s.Close()
	}
}
