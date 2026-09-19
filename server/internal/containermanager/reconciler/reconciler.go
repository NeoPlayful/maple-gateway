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
	pending       map[string]pendingRpl // 已创建但尚未就绪的副本（instance_id → 元数据）
	failures      map[string]int        // deployment_id → 连续失败次数（达上限即判 failed）
	failedDeploys map[string]bool       // deployment_id → 是否已因连续失败判停
}

// pendingRpl 是一次"已下发创建、尚未就绪"的副本。
type pendingRpl struct {
	versionID    string
	deploymentID string
	createdAt    time.Time
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

// maxFailures 是单个部署的连续失败上限：达到即判 failed 并停止补副本，
// 避免崩溃副本触发无限重建。
const maxFailures = 5

// Reconcile 执行一轮对账：先用 rollout.Plan 生成有序操作（先起后停），再逐条执行。
func (r *Reconciler) Reconcile(ctx context.Context) {
	states := r.desired.All()
	actual := r.actual.Snapshot()

	// 按版本聚合实际容器：version_id → []容器（含非 running，供删除与回收区分）。
	byVersion := map[string][]observer.ObservedContainer{}
	running := map[string]int{} // 仅健康 running，供收敛重置与失败判定
	for _, ac := range actual {
		vid := ac.Container.Labels["maple.version_id"]
		if vid == "" {
			continue
		}
		byVersion[vid] = append(byVersion[vid], ac)
		if ac.Container.State == "running" {
			running[vid]++
		}
	}
	// 生效副本数 = 健康 running + 在途副本。在途副本（已创建、尚未 running）计入实际数，
	// 避免"创建 → 观测滞后"窗口内重复创建导致超配。
	effective := map[string]int{}
	for vid, n := range running {
		effective[vid] = n
	}

	// 处理在途副本：就绪即结清；未就绪即崩溃则回收并计失败；超时未出现同样计失败。
	var dead []observer.ObservedContainer
	var failDepIDs []string
	r.mu.Lock()
	for iid, pr := range r.pending {
		ac, seen := actual[iid]
		switch {
		case !seen:
			if time.Since(pr.createdAt) > pendingTTL {
				delete(r.pending, iid) // 超时未被观测到，视为创建失败
				delete(r.instanceOf, iid)
				failDepIDs = append(failDepIDs, pr.deploymentID)
			} else {
				effective[pr.versionID]++
			}
		case ac.Container.State == "running":
			delete(r.pending, iid) // 已就绪，交由实际态计数
		case isTerminal(ac.Container.State):
			delete(r.pending, iid) // 未就绪即崩溃：回收容器并计失败
			delete(r.instanceOf, iid)
			dead = append(dead, ac)
			failDepIDs = append(failDepIDs, pr.deploymentID)
		default:
			effective[pr.versionID]++ // created/starting：仍在启动，窗口内计入
		}
	}
	r.mu.Unlock()

	for _, dep := range failDepIDs {
		r.noteFailure(dep, "", "replica did not become healthy")
	}
	for _, c := range dead {
		r.removeDead(ctx, c)
	}

	// 人工维护的实例计入实际副本数：管理员手动停掉的实例不再被对账器补回。
	for vid, n := range r.desired.PausedCount() {
		effective[vid] += n
	}

	// 生成有序操作：同部署内 surge 全部先于 drain（先起后停，保最低可用数）。
	ops := rollout.Plan(states, effective)
	placement := r.placementByNode(actual)
	for _, op := range ops {
		cur := byVersion[op.State.VersionID.String()]
		switch op.Kind {
		case rollout.KindSurge:
			// 该部署已因连续失败判停：不再补副本，避免崩溃容器触发无限重建。
			if r.isFailed(op.State.DeploymentID.String()) {
				continue
			}
			r.surge(ctx, op, placement)
		case rollout.KindDrain:
			r.drain(ctx, cur, op.Count)
		}
	}

	// 结算编排进度：把本轮结果聚合为各部署的 phase，供管理端展示。
	r.settlePhases(states, effective, running)
}

// settlePhases 依据本轮收敛情况回写各部署的编排阶段：
//
//	任一版本存在连续失败 → failed
//	所有版本实际数（含在途与人工维护）== 期望数 → ready
//	仍有差异（本轮已下发增删，待下轮观测确认） → reconciling
//
// 健康副本达标即视为收敛：清除该部署累积的失败计数，恢复自动补拉。
func (r *Reconciler) settlePhases(states []desired.State, effective, running map[string]int) {
	// 按部署聚合其版本目标与实际，判定该部署整体是否收敛。
	type agg struct {
		target, actual, healthy int
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
		a.actual += effective[st.VersionID.String()]
		a.healthy += running[st.VersionID.String()]
	}

	for did, a := range byDeploy {
		if a.healthy >= a.target {
			r.mu.Lock()
			delete(r.failures, did.String())
			delete(r.failedDeploys, did.String())
			r.mu.Unlock()
		}
		r.mu.Lock()
		failed := r.failedDeploys[did.String()]
		r.mu.Unlock()

		var phase desired.Phase
		switch {
		case failed:
			phase = desired.Phase{Status: "failed", Message: "副本创建连续失败"}
		case a.target == a.actual:
			phase = desired.Phase{Status: "ready"}
		default:
			phase = desired.Phase{Status: "reconciling"}
		}
		r.desired.SetPhase(did, phase)
	}
}

// isFailed 报告某部署是否已因连续失败判停（不再补副本）。
func (r *Reconciler) isFailed(deploymentID string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.failedDeploys[deploymentID]
}

// isTerminal 报告容器状态是否为"已终止"（不会再自行转入 running）。
func isTerminal(state string) bool {
	switch state {
	case "exited", "dead", "removing":
		return true
	default:
		return false
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
		Mounts:       toGwMounts(st.Mounts),
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
	// 登记为在途副本：就绪（running）或超时前计入实际副本数，避免窗口内重复创建。
	r.pending[instanceID] = pendingRpl{
		versionID:    st.VersionID.String(),
		deploymentID: st.DeploymentID.String(),
		createdAt:    time.Now(),
	}
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

// toGwMounts 把期望态挂载项转为下发 Agent 的形态。
func toGwMounts(ms []desired.Mount) []gwclient.Mount {
	if len(ms) == 0 {
		return nil
	}
	out := make([]gwclient.Mount, 0, len(ms))
	for _, m := range ms {
		out = append(out, gwclient.Mount{Path: m.Path, Target: m.Target, ReadOnly: m.ReadOnly})
	}
	return out
}

// placementByNode 统计各节点当前承载的受管容器数（调度打散依据）。
func (r *Reconciler) placementByNode(actual map[string]observer.ObservedContainer) map[string]int {
	out := map[string]int{}
	for _, c := range actual {
		out[c.NodeName]++
	}
	return out
}

// removeDead 强制删除一个"创建后未就绪即崩溃"的副本，避免残留死容器与重复占用。
// 仅针对仍在在途登记中的副本，故不会误删人工停用（paused）的实例。
func (r *Reconciler) removeDead(ctx context.Context, c observer.ObservedContainer) {
	node, ok := r.registry.Get(c.NodeName)
	if !ok {
		return
	}
	cctx, cancel := context.WithTimeout(ctx, replicaCallTimeout)
	defer cancel()
	if _, err := node.Call(cctx, agentprotocol.ActionContainerRemove, agentprotocol.RemoveParams{ID: c.InstanceID, Force: true}); err != nil {
		r.logger.Warn("recycle dead replica failed",
			zap.String("instance_id", c.InstanceID), zap.String("node", c.NodeName), zap.Error(err))
		return
	}
	r.logger.Info("dead replica recycled",
		zap.String("instance_id", c.InstanceID), zap.String("node", c.NodeName))
}

// noteFailure 记录一次失败并按部署聚合（有界重试，不无限重试）。
// 连续失败达上限即把该部署标记为 failed，surge 随之停止。
func (r *Reconciler) noteFailure(deploymentID, instanceID, msg string) {
	r.mu.Lock()
	r.failures[deploymentID]++
	n := r.failures[deploymentID]
	if n >= maxFailures {
		r.failedDeploys[deploymentID] = true
	}
	r.mu.Unlock()
	if n >= maxFailures {
		r.logger.Error("replica repeatedly failed; stop surge for deployment",
			zap.String("deployment_id", deploymentID),
			zap.String("instance_id", instanceID), zap.Int("attempts", n), zap.String("err", msg))
	} else {
		r.logger.Warn("replica create failed",
			zap.String("deployment_id", deploymentID),
			zap.String("instance_id", instanceID), zap.Int("attempts", n), zap.String("err", msg))
	}
}
