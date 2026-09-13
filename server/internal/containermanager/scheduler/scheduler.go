// Package scheduler 在节点间为副本挑选落点。
//
// 简单调度：按 node_selector（region/labels）过滤候选 → 排除不可用节点 →
// 在候选内按"已放置最少容器数"打散选择，尽量让副本分散到不同节点。
package scheduler

import (
	"github.com/NeoPlayful/maple-gateway/server/internal/containermanager/agentregistry"
)

// Scheduler 依据节点注册表与当前放置分布选择落点。
type Scheduler struct {
	registry *agentregistry.Registry
}

// New 构造。
func New(registry *agentregistry.Registry) *Scheduler {
	return &Scheduler{registry: registry}
}

// Select 从候选节点中选出 n 个落点（可重复，副本数超过节点数时回填）。
// selector 为调度约束（预期匹配节点 Labels）；placement 为各节点当前已放置的容器数。
// 返回的节点按"已放置数升序"排列，实现打散。
func (s *Scheduler) Select(selector map[string]string, placement map[string]int, n int) []*agentregistry.Node {
	if n <= 0 {
		return nil
	}
	candidates := make([]*agentregistry.Node, 0)
	for _, node := range s.registry.All() {
		if !node.Online() {
			continue
		}
		if !matchesSelector(node.Labels, selector) {
			continue
		}
		candidates = append(candidates, node)
	}
	if len(candidates) == 0 {
		return nil
	}
	// 按已放置容器数升序（打散）；同数保持配置顺序稳定。
	order := make([]int, len(candidates))
	for i := range order {
		order[i] = i
	}
	stableSortByPlacement(order, candidates, placement)

	out := make([]*agentregistry.Node, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, candidates[order[i%len(order)]])
	}
	return out
}

// matchesSelector 判断节点标签是否满足调度约束（selector 为空则全部匹配）。
func matchesSelector(labels, selector map[string]string) bool {
	for k, v := range selector {
		if labels[k] != v {
			return false
		}
	}
	return true
}

// stableSortByPlacement 按已放置容器数升序稳定排序索引。
func stableSortByPlacement(order []int, nodes []*agentregistry.Node, placement map[string]int) {
	for i := 1; i < len(order); i++ {
		for j := i; j > 0; j-- {
			a, b := order[j-1], order[j]
			if placement[nodes[a].Name] <= placement[nodes[b].Name] {
				break
			}
			order[j-1], order[j] = b, a
		}
	}
}
