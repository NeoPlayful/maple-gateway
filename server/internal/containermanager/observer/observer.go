// Package observer 周期采集各节点 Agent 上的容器实际态，并上报 Gateway。
//
// 职责（状态上行）：节点注册/心跳 → 容器发现 → 实例注册/心跳/注销。
// 容器 maple.instance_id 标签 = Gateway instances.id（同 UUID），保证一一对应。
package observer

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/NeoPlayful/maple-gateway/server/internal/containermanager/agentregistry"
	"github.com/NeoPlayful/maple-gateway/server/internal/containermanager/gwclient"
	"go.uber.org/zap"
)

// Observer 周期观测节点与容器并上报 Gateway。
type Observer struct {
	registry *agentregistry.Registry
	gw       *gwclient.GatewayClient
	interval time.Duration
	logger   *zap.Logger

	mu          sync.Mutex
	lastReport  time.Time                            // 上次成功上报时间
	lastErr     error                                // 上次上报错误
	containerCt int                                  // 纳管容器数
	nodeUpCt    int                                  // 可用节点数
	snapshot    map[string]ObservedContainer         // instance_id → 观测到的容器（最近一轮）
}

// ObservedContainer 是最近一轮观测到的受管容器（含其所在节点）。
type ObservedContainer struct {
	NodeName   string
	InstanceID string
	Container  gwclient.Container
}

// New 构造。
func New(registry *agentregistry.Registry, gw *gwclient.GatewayClient, interval time.Duration, logger *zap.Logger) *Observer {
	return &Observer{
		registry: registry,
		gw:       gw,
		interval: interval,
		logger:   logger,
		snapshot: map[string]ObservedContainer{},
	}
}

// Run 启动周期观测循环，直到 ctx 取消。
func (o *Observer) Run(ctx context.Context) {
	// 启动即先跑一轮，缩短首帧延迟；随周期定时。
	o.tick(ctx)
	ticker := time.NewTicker(o.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			o.tick(ctx)
		}
	}
}

// tick 执行一轮观测与上报。
func (o *Observer) tick(ctx context.Context) {
	nodes := o.registry.All()
	upCt, ct := 0, 0
	var firstErr error
	snap := make(map[string]ObservedContainer)
	// 本轮成功观测的节点集合：仅对这些节点做"消失即注销"，避免节点瞬时不可达误删其余实例。
	observedNodes := map[string]bool{}
	for _, n := range nodes {
		containers, err := o.observeNode(ctx, n)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			o.logger.Warn("observe node failed", zap.String("node", n.Name), zap.Error(err))
			continue
		}
		upCt++
		ct += len(containers)
		observedNodes[n.Name] = true
		for _, c := range containers {
			if c.InstanceID != "" {
				snap[c.InstanceID] = ObservedContainer{NodeName: n.Name, InstanceID: c.InstanceID, Container: c}
			}
		}
		o.reportNode(ctx, n, containers)
	}

	o.mu.Lock()
	prev := o.snapshot
	o.nodeUpCt = upCt
	o.containerCt = ct
	o.snapshot = snap
	if firstErr == nil {
		o.lastReport = time.Now()
		o.lastErr = nil
	} else {
		o.lastErr = firstErr
	}
	o.mu.Unlock()

	// 消失注销：上一轮见过、本轮未见于"已成功观测节点"上的实例 → 通知 Gateway 注销。
	for iid, pc := range prev {
		if !observedNodes[pc.NodeName] {
			continue // 该节点本轮未成功观测，不能据此判定实例消失
		}
		if _, still := snap[iid]; still {
			continue
		}
		if o.gw.Enabled() {
			if err := o.gw.DeleteInstance(ctx, iid); err != nil {
				o.logger.Warn("delete vanished instance via gateway failed",
					zap.String("instance_id", iid), zap.Error(err))
			} else {
				o.logger.Info("vanished instance deregistered",
					zap.String("instance_id", iid), zap.String("node", pc.NodeName))
			}
		}
	}
}

// Snapshot 返回最近一轮观测到的容器（instance_id → 容器），供对账器读取实际态。
func (o *Observer) Snapshot() map[string]ObservedContainer {
	o.mu.Lock()
	defer o.mu.Unlock()
	out := make(map[string]ObservedContainer, len(o.snapshot))
	for k, v := range o.snapshot {
		out[k] = v
	}
	return out
}

// observeNode 探测节点并采集容器列表；同时更新注册表健康状态。
func (o *Observer) observeNode(ctx context.Context, n *agentregistry.Node) ([]gwclient.Container, error) {
	if err := n.Agent.Health(ctx); err != nil {
		o.registry.SetHealth(n.Name, false, "", time.Now().UnixMilli())
		return nil, err
	}
	containers, err := n.Agent.Containers(ctx)
	if err != nil {
		o.registry.SetHealth(n.Name, false, "", time.Now().UnixMilli())
		return nil, err
	}
	o.registry.SetHealth(n.Name, true, "", time.Now().UnixMilli())
	return containers, nil
}

// reportNode 上报节点注册/心跳，并把该节点上的容器作为实例上报。
func (o *Observer) reportNode(ctx context.Context, n *agentregistry.Node, containers []gwclient.Container) {
	if !o.gw.Enabled() {
		return
	}
	// 节点注册（首次）或心跳（已有 ID）。
	nodeID := n.GatewayID
	if nodeID == "" {
		id, err := o.gw.RegisterNode(ctx, n.Name, n.Host, n.Region, n.Labels)
		if err != nil {
			o.logger.Warn("register node to gateway failed", zap.String("node", n.Name), zap.Error(err))
			return
		}
		o.registry.SetHealth(n.Name, true, id, time.Now().UnixMilli())
		nodeID = id
	} else if err := o.gw.HeartbeatNode(ctx, nodeID); err != nil {
		o.logger.Warn("node heartbeat to gateway failed", zap.String("node", n.Name), zap.Error(err))
	}

	// 容器 → 实例上报：仅上报 running 状态、且带 instance_id 标签的受管容器。
	sort.Slice(containers, func(i, j int) bool { return containers[i].InstanceID < containers[j].InstanceID })
	for _, ct := range containers {
		if ct.InstanceID == "" || ct.State != "running" {
			continue
		}
		o.reportInstance(ctx, n, nodeID, ct)
	}
}

// reportInstance 上报单个容器对应的实例：优先心跳（已注册），失败则注册。
func (o *Observer) reportInstance(ctx context.Context, n *agentregistry.Node, nodeID string, ct gwclient.Container) {
	// 直接用 instance_id 作心跳：容器标签即 Gateway 实例 ID。
	if err := o.gw.HeartbeatInstance(ctx, ct.InstanceID); err == nil {
		return
	}
	// 心跳失败（实例尚不存在）→ 注册。
	rep := gwclient.InstanceReport{
		ID:           ct.InstanceID,
		ServiceID:    ct.Labels["maple.service_id"],
		DeploymentID: ct.Labels["maple.deployment_id"],
		VersionID:    ct.Labels["maple.version_id"],
		NodeID:       nodeID,
		Address:      n.Host,
		Port:         ct.HostPort,
		Protocol:     "http",
	}
	if _, err := o.gw.RegisterInstance(ctx, rep); err != nil {
		o.logger.Warn("register instance to gateway failed",
			zap.String("node", n.Name), zap.String("instance_id", ct.InstanceID), zap.Error(err))
	}
}

// Stats 返回观测统计（供健康/指标暴露）。
type Stats struct {
	NodeUp       int       `json:"node_up"`
	Containers   int       `json:"containers"`
	LastReportAt time.Time `json:"last_report_at"`
	LastError    string    `json:"last_error,omitempty"`
}

// Stats 返回当前观测统计。
func (o *Observer) Stats() Stats {
	o.mu.Lock()
	defer o.mu.Unlock()
	s := Stats{
		NodeUp:       o.nodeUpCt,
		Containers:   o.containerCt,
		LastReportAt: o.lastReport,
	}
	if o.lastErr != nil {
		s.LastError = o.lastErr.Error()
	}
	return s
}
