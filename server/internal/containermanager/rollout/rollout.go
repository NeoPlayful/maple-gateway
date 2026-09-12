// Package rollout 把"期望态 vs 实际态"的差异编排成有序操作，落实容器侧发布形态。
//
// 核心不变式：**先起后停**（surge before drain）——新版本容器就绪后再移除旧版本，
// 保证滚动/蓝绿过程中最低可用数不被击穿。三种形态共用该不变式，差异体现在
// 版本角色（stable/active/canary/standby）与副本目标的组合上：
//
//	Rolling    新旧版本各自到位，扩先于缩，逐版本交接；
//	Blue/Green standby 版本整组起好（扩），切换后旧版本转 inactive 再缩；
//	Canary     canary 版本按目标副本数扩，旧版本保持不变。
package rollout

import (
	"sort"

	"github.com/NeoPlayful/maple-gateway/server/internal/containermanager/desired"
)

// Kind 是操作类型。
type Kind string

const (
	KindSurge Kind = "surge" // 创建副本
	KindDrain Kind = "drain" // 移除副本
)

// Op 是一条编排操作：对某版本增/减 Count 个副本。
type Op struct {
	Kind  Kind
	State desired.State
	Count int
}

// Actual 是某版本当前 running 的副本数（由观测得出）。
type Actual map[string]int // version_id → running 数

// Plan 依据期望态与实际态生成有序操作列表。
// 同一部署内：先全部 surge（扩），再全部 drain（缩），确保先起后停。
// 部署之间按 deployment_id 排序，保证顺序稳定可复现。
func Plan(states []desired.State, actual Actual) []Op {
	byDeploy := map[string][]desired.State{}
	deployOrder := []string{}
	for _, st := range states {
		did := st.DeploymentID.String()
		if _, ok := byDeploy[did]; !ok {
			deployOrder = append(deployOrder, did)
		}
		byDeploy[did] = append(byDeploy[did], st)
	}
	sort.Strings(deployOrder)

	var surges, drains []Op
	for _, did := range deployOrder {
		group := byDeploy[did]
		// 稳定排序：按版本号，保证同部署内顺序确定。
		sort.Slice(group, func(i, j int) bool { return group[i].Version < group[j].Version })
		for _, st := range group {
			target := st.Replicas
			if target < 0 {
				target = 0
			}
			cur := actual[st.VersionID.String()]
			switch {
			case target > cur:
				surges = append(surges, Op{Kind: KindSurge, State: st, Count: target - cur})
			case target < cur:
				drains = append(drains, Op{Kind: KindDrain, State: st, Count: cur - target})
			}
		}
	}
	// 先起后停：所有 surge 在前，drain 在后。
	return append(surges, drains...)
}

// IsRetiring 判断版本角色是否属于"待退役"（发布时优先被缩容的一方）。
func IsRetiring(status string) bool {
	switch status {
	case "inactive", "draining", "standby":
		return true
	default:
		return false
	}
}
