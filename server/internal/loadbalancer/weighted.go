// Package loadbalancer 从健康实例池中选择转发目标。
//
// 支持 Round Robin（各实例 weight 相同时退化为标准 RR）与平滑加权轮询。
// 状态按路由 key（DomainID）独立维护；池实例集合变化时自动复位。
package loadbalancer

import (
	"strconv"
	"strings"
	"sync"

	"github.com/NeoPlayful/maple-gateway/server/internal/router"
)

// Balancer 维护每个路由条目的加权轮询状态 + 全局 Least-Connections 计数。
type Balancer struct {
	mu    sync.Mutex
	byKey map[string]*wrrState
	lc    *leastConnTracker // Least-Connections 在途连接计数
}

// NewBalancer 构造。
func NewBalancer() *Balancer {
	return &Balancer{byKey: map[string]*wrrState{}, lc: newLeastConnTracker()}
}

// IncConn 记录请求转发到指定实例（Least-Connections 选路计数 +1）。
func (b *Balancer) IncConn(instanceID string) { b.lc.IncConn(instanceID) }

// DecConn 记录请求结束（Least-Connections 计数 -1）。
func (b *Balancer) DecConn(instanceID string) { b.lc.DecConn(instanceID) }

// SelectLeastConnections 从池中选在途连接最少者（Least-Connections 策略）。
func (b *Balancer) SelectLeastConnections(pool []router.PoolMember) *router.PoolMember {
	return LeastConnections(pool, b.lc)
}

// Select 从 pool 选一个实例。key 为路由标识（DomainID）；pool 为空返回 nil。
// 指纹含实例 ID：实例集合增删会复位轮询状态，保证 current 数组与 index 映射一致。
func (b *Balancer) Select(key string, pool []router.PoolMember) *router.PoolMember {
	if len(pool) == 0 {
		return nil
	}
	b.mu.Lock()
	defer b.mu.Unlock()

	s, ok := b.byKey[key]
	if !ok {
		s = &wrrState{}
		b.byKey[key] = s
	}
	if s.fingerprint != poolFingerprint(pool) {
		s.reset(pool)
	}
	return &pool[s.next()]
}

// PickWeighted 从一组权重中按平滑加权轮询选出一个下标。
// key 区分不同路由/层级（如 DomainID 或 DomainID+version 层）；weights 为空返回 -1。
func (b *Balancer) PickWeighted(key string, weights []int) int {
	if len(weights) == 0 {
		return -1
	}
	b.mu.Lock()
	defer b.mu.Unlock()

	s, ok := b.byKey[key]
	if !ok {
		s = &wrrState{}
		b.byKey[key] = s
	}
	if s.fingerprint != weightsFingerprint(weights) {
		s.resetWeights(weights)
	}
	return s.next()
}

// wrrState 平滑加权轮询单池状态（nginx 风格）。
type wrrState struct {
	fingerprint string
	weight      []int
	current     []int
	total       int
}

func (s *wrrState) reset(pool []router.PoolMember) {
	s.resetWeights(memberWeights(pool))
	// 池指纹含实例 ID：实例集合变化也必须复位（当前数组随 index 对齐）。
	s.fingerprint = poolFingerprint(pool)
}

// resetWeights 直接用权重切片初始化轮询状态，并记录权重指纹。
func (s *wrrState) resetWeights(weights []int) {
	s.fingerprint = weightsFingerprint(weights)
	s.weight = make([]int, len(weights))
	s.current = make([]int, len(weights))
	s.total = 0
	for i, w := range weights {
		if w <= 0 {
			w = 1
		}
		s.weight[i] = w
		s.current[i] = 0
		s.total += w
	}
}

func memberWeights(pool []router.PoolMember) []int {
	ws := make([]int, len(pool))
	for i, m := range pool {
		ws[i] = m.Weight
	}
	return ws
}

func weightsFingerprint(weights []int) string {
	var sb strings.Builder
	for _, w := range weights {
		if w <= 0 {
			w = 1
		}
		sb.WriteString(strconv.Itoa(w))
		sb.WriteByte(',')
	}
	return sb.String()
}

// next 返回选中实例的索引。每轮 current[i] += weight[i]，选累计最大者，
// 再让该实例减去 total；weight 全相等时退化为标准 RR。
func (s *wrrState) next() int {
	best := 0
	for i := range s.weight {
		s.current[i] += s.weight[i]
		if s.current[i] > s.current[best] {
			best = i
		}
	}
	s.current[best] -= s.total
	return best
}

// poolFingerprint 用有序实例 ID 与权重生成池指纹；
// 实例增删或权重变化都会导致指纹不同并复位轮询状态。
func poolFingerprint(pool []router.PoolMember) string {
	var sb strings.Builder
	for _, m := range pool {
		sb.WriteString(m.ID)
		sb.WriteByte('|')
		w := m.Weight
		if w <= 0 {
			w = 1
		}
		sb.WriteString(strconv.Itoa(w))
		sb.WriteByte(',')
	}
	return sb.String()
}
