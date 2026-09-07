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

// Balancer 维护每个路由条目的加权轮询状态。
type Balancer struct {
	mu    sync.Mutex
	byKey map[string]*wrrState
}

// NewBalancer 构造。
func NewBalancer() *Balancer {
	return &Balancer{byKey: map[string]*wrrState{}}
}

// Select 从 pool 选一个实例。key 为路由标识（DomainID）；pool 为空返回 nil。
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

// wrrState 平滑加权轮询单池状态（nginx 风格）。
type wrrState struct {
	fingerprint string
	weight      []int
	current     []int
	total       int
}

func (s *wrrState) reset(pool []router.PoolMember) {
	s.fingerprint = poolFingerprint(pool)
	s.weight = make([]int, len(pool))
	s.current = make([]int, len(pool))
	s.total = 0
	for i, m := range pool {
		w := m.Weight
		if w <= 0 {
			w = 1
		}
		s.weight[i] = w
		s.current[i] = 0
		s.total += w
	}
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
