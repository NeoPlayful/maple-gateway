// Package reconciler 让各部署的实际态向期望态收敛。
//
// 周期遍历期望态（来自 Gateway 下发的部署意图），按 instance_id 幂等创建/删除容器：
//
//	缺副本 → 调度节点 → Agent 创建容器（Agent 注入 instance_id 标签）
//	多副本 → 先经 Gateway 通知 drain → Agent 优雅停止 → 删除
//	一致   → 不动
//
// 期望态中的每个版本按其 replicas 目标维持容器数；instance_id 由 CM 预生成并作为
// 幂等键与容器标签，保证"容器 ↔ Gateway 实例"一一对应。
package reconciler

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/NeoPlayful/maple-gateway/server/internal/agentprotocol"
	"github.com/NeoPlayful/maple-gateway/server/internal/containermanager/agentregistry"
	"github.com/NeoPlayful/maple-gateway/server/internal/containermanager/desired"
	"github.com/NeoPlayful/maple-gateway/server/internal/containermanager/gwclient"
	"github.com/NeoPlayful/maple-gateway/server/internal/containermanager/observer"
	"github.com/NeoPlayful/maple-gateway/server/internal/containermanager/rollout"
	"github.com/NeoPlayful/maple-gateway/server/internal/containermanager/scheduler"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

// DesiredReader 提供期望态列表（由 desired.Store 实现）。
type DesiredReader interface {
	All() []desired.State
	PausedCount() map[string]int // 人工置为维护的实例数（version_id → 数量）
	SetPhase(deploymentID uuid.UUID, phase desired.Phase)
}

// ActualReader 提供实际态容器快照（由 observer.Observer 实现）。
type ActualReader interface {
	Snapshot() map[string]observer.ObservedContainer
}

// Reconciler 周期对账期望态与实际态。
type Reconciler struct {
	desired  DesiredReader
	actual   ActualReader
	registry *agentregistry.Registry
	gw       *gwclient.GatewayClient
	sched    *scheduler.Scheduler
	interval time.Duration
	logger   *zap.Logger

	// instance 记录：instance_id → 其归属部署/版本（CM 生成 instance_id 时登记）。
	mu            sync.Mutex
	instanceOf    map[string]string     // instance_id → version_id
	pending       map[string]pendingRpl // 已创建但观测尚未确认的副本（instance_id → 元数据）
	failures      map[string]int        // instance_id → 连续失败次数（退避/告警）
	failedDeploys map[string]bool       // deployment_id → 是否已因连续失败标记为 failed
}

// pendingRpl 是一次"已下发创建、尚待观测确认"的副本。
type pendingRpl struct {
	versionID string
	createdAt time.Time
}

// pendingTTL 是 pending 副本的存活上限：超过仍未被观测确认则视为创建失败并丢弃。
const pendingTTL = 60 * time.Second

// New 构造。
func New(d DesiredReader, a ActualReader, registry *agentregistry.Registry, gw *gwclient.GatewayClient, interval time.Duration, logger *zap.Logger) *Reconciler {
	return &Reconciler{
		desired:       d,
		actual:        a,
		registry:      registry,
		gw:            gw,
		sched:         scheduler.New(registry),
		interval:      interval,
		logger:        logger,
		instanceOf:    map[string]string{},
		pending:       map[string]pendingRpl{},
		failures:      map[string]int{},
		failedDeploys: map[string]bool{},
	}
}

// Run 启动对账循环，直到 ctx 取消。
func (r *Reconciler) Run(ctx context.Context) {
	ticker := time.NewTicker(r.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.Reconcile(ctx)
		}
	}
}

// maxFailures 是单个 instance 的连续失败上限，超过不再无限重试。
const maxFailures = 5

// Reconcile 执行一轮对账：先用 rollout.Plan 生成有序操作（先起后停），再逐条执行。
func (r *Reconciler) Reconcile(ctx context.Context) {
	states := r.desired.All()
	actual := r.actual.Snapshot()

	// 按版本聚合实际容器：version_id → []容器（含非 running，供删除时区分）。
	byVersion := map[string][]observer.ObservedContainer{}
	runningCount := map[string]int{}
	for _, ac := range actual {
		vid := ac.Container.Labels["maple.version_id"]
		if vid == "" {
			continue
		}
		byVersion[vid] = append(byVersion[vid], ac)
		if ac.Container.State == "running" {
			runningCount[vid]++
		}
	}

	// 归并 in-flight 副本：已下发创建但观测尚未确认的，计入实际数，
	// 避免"创建 → 观测滞后"窗口内重复创建导致超配。同时清理超时未确认的 pending。
	r.mu.Lock()
	for iid, pr := range r.pending {
		if _, seen := actual[iid]; seen {
			delete(r.pending, iid) // 已被观测确认，交由实际态计数
			continue
		}
		if time.Since(pr.createdAt) > pendingTTL {
			delete(r.pending, iid) // 超时未确认，视为失败丢弃
			r.instanceOf[iid] = "" // 保留失败记录
			continue
		}
		runningCount[pr.versionID]++
	}
	r.mu.Unlock()

	// 人工维护的实例计入实际副本数：管理员手动停掉的实例不再被对账器补回。
	for vid, n := range r.desired.PausedCount() {
		runningCount[vid] += n
	}

	// 生成有序操作：同部署内 surge 全部先于 drain（先起后停，保最低可用数）。
	ops := rollout.Plan(states, runningCount)
	placement := r.placementByNode(actual)
	for _, op := range ops {
		cur := byVersion[op.State.VersionID.String()]
		switch op.Kind {
		case rollout.KindSurge:
			r.surge(ctx, op, placement)
		case rollout.KindDrain:
			r.drain(ctx, cur, op.Count)
		}
	}

	// 结算编排进度：把本轮结果聚合为各部署的 phase，供管理端展示。
	r.settlePhases(states, runningCount)
}

// settlePhases 依据本轮收敛情况回写各部署的编排阶段：
//
//	任一版本存在连续失败 → failed
//	所有版本实际数（含 in-flight 与人工维护）== 期望数 → ready
//	仍有差异（本轮已下发增删，待下轮观测确认） → reconciling
func (r *Reconciler) settlePhases(states []desired.State, runningCount map[string]int) {
	// 按部署聚合其版本目标与实际，判定该部署整体是否收敛。
	type agg struct {
		target, actual int
		failed         bool
	}
	byDeploy := map[uuid.UUID]*agg{}
	for _, st := range states {
		a := byDeploy[st.DeploymentID]
		if a == nil {
			a = &agg{}
			byDeploy[st.DeploymentID] = a
		}
		target := st.Replicas
		if target < 0 {
			target = 0
		}
		a.target += target
		a.actual += runningCount[st.VersionID.String()]
	}

	r.mu.Lock()
	failed := make(map[string]bool, len(r.failedDeploys))
	for k, v := range r.failedDeploys {
		failed[k] = v
	}
	r.mu.Unlock()

	for did, a := range byDeploy {
		var phase desired.Phase
		switch {
		case failed[did.String()]:
			phase = desired.Phase{Status: "failed", Message: "副本创建连续失败"}
		case a.target == a.actual:
			phase = desired.Phase{Status: "ready"}
		default:
			phase = desired.Phase{Status: "reconciling"}
		}
		r.desired.SetPhase(did, phase)
	}
}

// surge 对某版本扩容 Count 个副本，按已放置分布打散落点。
func (r *Reconciler) surge(ctx context.Context, op rollout.Op, placement map[string]int) {
	nodes := r.sched.Select(op.State.NodeSelector, placement, op.Count)
	if len(nodes) == 0 {
		r.logger.Warn("no schedulable node for deployment",
			zap.String("deployment_id", op.State.DeploymentID.String()))
		return
	}
	for i := 0; i < op.Count; i++ {
		node := nodes[i%len(nodes)]
		r.createReplica(ctx, op.State, node)
		placement[node.Name]++
	}
}

// drain 从某版本的多余 running 容器中删除 Count 个（按 instance_id 稳定排序）。
func (r *Reconciler) drain(ctx context.Context, containers []observer.ObservedContainer, count int) {
	// 仅对 running 容器做删减（停止/退出的留待后续清理）。
	running := make([]observer.ObservedContainer, 0, len(containers))
	for _, c := range containers {
		if c.Container.State == "running" {
			running = append(running, c)
		}
	}
	sort.Slice(running, func(i, j int) bool { return running[i].InstanceID < running[j].InstanceID })
	if count > len(running) {
		count = len(running)
	}
	for i := 0; i < count; i++ {
		r.removeReplica(ctx, running[i])
	}
}

// replicaCallTimeout 是单次副本操作（创建/停止/删除）的等待上限。
const replicaCallTimeout = 60 * time.Second

// createReplica 在一个节点上创建一份副本：预生成 instance_id → 经 WS 通道下发创建。
func (r *Reconciler) createReplica(ctx context.Context, st desired.State, node *agentregistry.Node) {
	instanceID := uuid.NewString()
	spec := gwclient.CreateSpec{
		InstanceID:   instanceID,
		ServiceID:    st.ServiceID.String(),
		DeploymentID: st.DeploymentID.String(),
		VersionID:    st.VersionID.String(),
		Image:        st.Image,
		Port:         st.Port,
		Env:          st.Env,
		HealthPath:   st.HealthPath,
	}
	cctx, cancel := context.WithTimeout(ctx, replicaCallTimeout)
	defer cancel()
	raw, err := node.Call(cctx, agentprotocol.ActionContainerCreate, spec)
	if err != nil {
		r.noteFailure(st.DeploymentID.String(), instanceID, fmt.Sprintf("create on node %s: %v", node.Name, err))
		return
	}
	var res agentprotocol.CreateResult
	_ = json.Unmarshal(raw, &res)
	r.mu.Lock()
	r.instanceOf[instanceID] = st.VersionID.String()
	// 登记为 in-flight：观测确认前计入实际数，避免窗口内重复创建。
	r.pending[instanceID] = pendingRpl{versionID: st.VersionID.String(), createdAt: time.Now()}
	delete(r.failures, instanceID)
	// 本部署有副本成功创建：清除先前标记的失败态，交回收敛判定。
	delete(r.failedDeploys, st.DeploymentID.String())
	r.mu.Unlock()
	r.logger.Info("replica created",
		zap.String("deployment_id", st.DeploymentID.String()),
		zap.String("instance_id", instanceID),
		zap.String("node", node.Name),
		zap.Int("host_port", res.HostPort))
}

// removeReplica 删除一份副本：先经 Gateway 通知 drain（先摘流量），再优雅停止容器。
func (r *Reconciler) removeReplica(ctx context.Context, c observer.ObservedContainer) {
	// 先摘流量：通知 Gateway 该实例进入 draining。
	if r.gw.Enabled() {
		if err := r.gw.DrainInstance(ctx, c.InstanceID); err != nil {
			r.logger.Warn("drain instance via gateway failed",
				zap.String("instance_id", c.InstanceID), zap.Error(err))
		}
	}
	node, ok := r.registry.Get(c.NodeName)
	if !ok {
		return
	}
	cctx, cancel := context.WithTimeout(ctx, replicaCallTimeout)
	defer cancel()
	// 后停容器：优雅停止（超时强杀），再删除。
	if _, err := node.Call(cctx, agentprotocol.ActionContainerStop, agentprotocol.IDParams{ID: c.InstanceID}); err != nil {
		r.logger.Warn("stop container failed", zap.String("instance_id", c.InstanceID), zap.Error(err))
	}
	if _, err := node.Call(cctx, agentprotocol.ActionContainerRemove, agentprotocol.RemoveParams{ID: c.InstanceID}); err != nil {
		// 停止可能未成功，退化为强删。
		if _, err := node.Call(cctx, agentprotocol.ActionContainerRemove, agentprotocol.RemoveParams{ID: c.InstanceID, Force: true}); err != nil {
			r.logger.Warn("remove container failed", zap.String("instance_id", c.InstanceID), zap.Error(err))
		}
	}
	r.mu.Lock()
	delete(r.instanceOf, c.InstanceID)
	delete(r.pending, c.InstanceID)
	r.mu.Unlock()
	r.logger.Info("replica removed",
		zap.String("instance_id", c.InstanceID), zap.String("node", c.NodeName))
}

// placementByNode 统计各节点当前承载的受管容器数（调度打散依据）。
func (r *Reconciler) placementByNode(actual map[string]observer.ObservedContainer) map[string]int {
	out := map[string]int{}
	for _, c := range actual {
		out[c.NodeName]++
	}
	return out
}

// noteFailure 记录一次失败并达上限时告警（有界重试，不无限重试）。
// deploymentID 用于把达上限的失败聚合为部署级 failed 阶段。
func (r *Reconciler) noteFailure(deploymentID, instanceID, msg string) {
	r.mu.Lock()
	r.failures[instanceID]++
	n := r.failures[instanceID]
	if n >= maxFailures {
		r.failedDeploys[deploymentID] = true
	}
	r.mu.Unlock()
	if n >= maxFailures {
		r.logger.Error("replica create repeatedly failed",
			zap.String("deployment_id", deploymentID),
			zap.String("instance_id", instanceID), zap.Int("attempts", n), zap.String("err", msg))
	} else {
		r.logger.Warn("replica create failed",
			zap.String("instance_id", instanceID), zap.Int("attempts", n), zap.String("err", msg))
	}
}
