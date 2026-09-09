package loadbalancer

import (
	"sync"

	"github.com/NeoPlayful/maple-gateway/server/internal/router"
)

// leastConnTracker 维护每个实例 ID 的在途连接计数。
// 由数据平面在请求进入/结束时调用 IncConn/DecConn；
// LeastConnections 选择时读取当前计数，选连接最少者（权重相同时兜底）。
type leastConnTracker struct {
	mu   sync.Mutex
	conn map[string]int64 // 实例 ID → 在途连接数
}

func newLeastConnTracker() *leastConnTracker {
	return &leastConnTracker{conn: map[string]int64{}}
}

// IncConn 请求转发到 instanceID 时 +1。
func (t *leastConnTracker) IncConn(instanceID string) {
	t.mu.Lock()
	t.conn[instanceID]++
	t.mu.Unlock()
}

// DecConn 请求完成/失败时 -1（不低于 0）。
func (t *leastConnTracker) DecConn(instanceID string) {
	t.mu.Lock()
	if v := t.conn[instanceID]; v > 0 {
		t.conn[instanceID] = v - 1
	}
	t.mu.Unlock()
}

// count 返回指定实例当前在途连接数（无记录视为 0）。
func (t *leastConnTracker) count(instanceID string) int64 {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.conn[instanceID]
}

// LeastConnections 从池中选在途连接最少者；同连接数时按权重加权回落（权重高优先）。
// 需 tracker 提供实时计数；池实例为 nil 时返回 nil。
func LeastConnections(pool []router.PoolMember, tracker *leastConnTracker) *router.PoolMember {
	if len(pool) == 0 {
		return nil
	}
	best := -1
	bestConn := int64(1 << 62)
	for i := range pool {
		id := pool[i].ID
		if id == "" {
			id = pool[i].Endpoint
		}
		c := tracker.count(id)
		// 连接更少的直接胜出；连接相同选权重更高者。
		if best == -1 || c < bestConn ||
			(c == bestConn && pool[i].Weight > pool[best].Weight) {
			best = i
			bestConn = c
		}
	}
	return &pool[best]
}
