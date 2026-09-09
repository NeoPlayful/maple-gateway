package loadbalancer

import (
	"hash/fnv"
	"sort"
	"strconv"

	"github.com/NeoPlayful/maple-gateway/server/internal/router"
)

// vnodeCount 每个实例在哈希环上的虚拟节点数（增大以均衡分布，随池规模权衡）。
const vnodeCount = 100

// hashRing 是带虚拟节点的一致性哈希环。
// 结构一经构造不可变；池变化时由调用方重建（每次选择重建开销低，池通常很小）。
type hashRing struct {
	points []uint32 // 环上点（升序）
	byPt   map[uint32]string
}

// newHashRing 从实例 ID 列表构建一致性哈希环。
func newHashRing(ids []string) *hashRing {
	r := &hashRing{byPt: map[uint32]string{}}
	seen := map[uint32]bool{}
	for _, id := range ids {
		for i := 0; i < vnodeCount; i++ {
			h := hashKey(id + "#" + strconv.Itoa(i))
			if seen[h] {
				continue
			}
			seen[h] = true
			r.byPt[h] = id
			r.points = append(r.points, h)
		}
	}
	sort.Slice(r.points, func(i, j int) bool { return r.points[i] < r.points[j] })
	return r
}

// lookup 返回 key 命中的实例 ID；环为空返回 ""。
func (r *hashRing) lookup(key string) string {
	if len(r.points) == 0 {
		return ""
	}
	h := hashKey(key)
	// 二分找第一个 >= h 的点；越界回绕到开头。
	i := sort.Search(len(r.points), func(i int) bool { return r.points[i] >= h })
	if i == len(r.points) {
		i = 0
	}
	return r.byPt[r.points[i]]
}

// ConsistentHash 用一致性哈希从池中按会话键选实例。
// 同 key 恒选同实例；实例增删时仅最小集合的 key 迁移到新实例。
// 权重在此策略下不参与（hash 场景按实例 ID 固定映射）。
func ConsistentHash(pool []router.PoolMember, key string) *router.PoolMember {
	if len(pool) == 0 {
		return nil
	}
	ids := make([]string, len(pool))
	byID := make(map[string]*router.PoolMember, len(pool))
	for i := range pool {
		id := pool[i].ID
		if id == "" {
			id = pool[i].Endpoint
		}
		ids[i] = id
		byID[id] = &pool[i]
	}
	ring := newHashRing(ids)
	if id := ring.lookup(key); id != "" {
		return byID[id]
	}
	return nil
}

func hashKey(s string) uint32 {
	h := fnv.New32a()
	_, _ = h.Write([]byte(s))
	return h.Sum32()
}
