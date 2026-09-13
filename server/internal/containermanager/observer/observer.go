// Package observer 周期采集各节点 Agent 上的容器实际态，并上报 Gateway。
//
// 职责（状态上行）：节点注册/心跳 → 容器发现 → 实例注册/心跳/注销。
// 容器 maple.instance_id 标签 = Gateway instances.id（同 UUID），保证一一对应。
//
// 采集经 WS 任务通道下发（container.list / system.info / node.metrics），
// 节点在线状态来自统一节点模型（有活跃 WS 会话才尝试采集），不再依赖 HTTP 探测。
package observer

import (
	"context"
	"encoding/json"
	"sort"
	"sync"
	"time"

	"github.com/NeoPlayful/maple-gateway/server/internal/agentprotocol"
	"github.com/NeoPlayful/maple-gateway/server/internal/containermanager/agentregistry"
	"github.com/NeoPlayful/maple-gateway/server/internal/containermanager/gwclient"
	"go.uber.org/zap"
)

// NodeInfo 是节点容量摘要（对应 Agent docker.NodeInfo）。
type NodeInfo struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	CPUs          int    `json:"cpus"`
	MemoryBytes   int64  `json:"memory_bytes"`
	Containers    int    `json:"containers"`
	DockerVersion string `json:"docker_version"`
}

// HostMetrics 是主机资源使用摘要（对应 Agent hostmetrics.Metrics）。
type HostMetrics struct {
	Available   bool    `json:"available"`
	CPUPercent  float64 `json:"cpu_percent"`
	MemTotal    int64   `json:"mem_total"`
	MemUsed     int64   `json:"mem_used"`
	MemPercent  float64 `json:"mem_percent"`
	DiskTotal   int64   `json:"disk_total"`
	DiskUsed    int64   `json:"disk_used"`
	DiskPercent float64 `json:"disk_percent"`
}

// DockerDisk 是 Docker 引擎空间占用摘要。
type DockerDisk struct {
	LayersSize int64 `json:"layers_size"`
	Images     int   `json:"images"`
	Containers int   `json:"containers"`
	Volumes    int   `json:"volumes"`
}

// NodeMetrics 是节点指标聚合（主机 + Docker）。
type NodeMetrics struct {
	Host   HostMetrics `json:"host"`
	Docker DockerDisk  `json:"docker"`
}

// Observer 周期观测节点与容器并上报 Gateway。
type Observer struct {
	registry *agentregistry.Registry
	gw       *gwclient.GatewayClient
	interval time.Duration
	logger   *zap.Logger

	mu          sync.Mutex
	lastReport  time.Time
	lastErr     error
	containerCt int
	nodeUpCt    int
	snapshot    map[string]ObservedContainer
	metrics     map[string]NodeMetric
	info        map[string]NodeInfo
	errors      []RuntimeError
}

// NodeMetric 是某节点最近一次采集到的资源指标。
type NodeMetric struct {
	NodeName string      `json:"node_name"`
	Metrics  NodeMetrics `json:"metrics"`
	Error    string      `json:"error,omitempty"`
}

// RuntimeError 是一次运行时异常（容器非正常退出）记录。
type RuntimeError struct {
	NodeName   string `json:"node_name"`
	InstanceID string `json:"instance_id"`
	State      string `json:"state"`
	ExitCode   int    `json:"exit_code"`
	OOMKilled  bool   `json:"oom_killed"`
	At         int64  `json:"at"`
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
		metrics:  map[string]NodeMetric{},
		info:     map[string]NodeInfo{},
	}
}

// Run 启动周期观测循环，直到 ctx 取消。
func (o *Observer) Run(ctx context.Context) {
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
	metrics := make(map[string]NodeMetric, len(nodes))
	infos := make(map[string]NodeInfo, len(nodes))
	var runtimeErrs []RuntimeError
	observedNodes := map[string]bool{}
	for _, n := range nodes {
		// 仅在节点有活跃 WS 会话、且已认领 Gateway 身份时才观测。
		if !n.Online() || n.ID() == "" {
			continue
		}
		metrics[n.Name] = o.collectMetrics(ctx, n)
		if info, err := o.fetchInfo(ctx, n); err == nil {
			infos[n.Name] = info
		}
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
			if c.InstanceID != "" && c.State != "running" {
				runtimeErrs = append(runtimeErrs, RuntimeError{
					NodeName: n.Name, InstanceID: c.InstanceID, State: c.State,
					ExitCode: c.ExitCode, OOMKilled: c.OOMKilled, At: time.Now().Unix(),
				})
			}
		}
		o.reportNode(ctx, n, containers)
	}

	o.mu.Lock()
	prev := o.snapshot
	o.nodeUpCt = upCt
	o.containerCt = ct
	o.snapshot = snap
	o.metrics = metrics
	o.info = infos
	o.errors = runtimeErrs
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
			continue
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

// Metrics 返回各节点最近一轮采集到的资源指标。
func (o *Observer) Metrics() map[string]NodeMetric {
	o.mu.Lock()
	defer o.mu.Unlock()
	out := make(map[string]NodeMetric, len(o.metrics))
	for k, v := range o.metrics {
		out[k] = v
	}
	return out
}

// Infos 返回各节点最近一轮采集到的容量信息（核数/内存/Docker 版本）。
func (o *Observer) Infos() map[string]NodeInfo {
	o.mu.Lock()
	defer o.mu.Unlock()
	out := make(map[string]NodeInfo, len(o.info))
	for k, v := range o.info {
		out[k] = v
	}
	return out
}

// RuntimeErrors 返回最近一轮观测到的运行时错误（容器非正常退出）。
func (o *Observer) RuntimeErrors() []RuntimeError {
	o.mu.Lock()
	defer o.mu.Unlock()
	out := make([]RuntimeError, len(o.errors))
	copy(out, o.errors)
	return out
}

// collectMetrics 采集单节点资源指标；失败时记录错误原因但不影响观测主流程。
func (o *Observer) collectMetrics(ctx context.Context, n *agentregistry.Node) NodeMetric {
	raw, err := o.call(ctx, n, agentprotocol.ActionNodeMetrics)
	if err != nil {
		return NodeMetric{NodeName: n.Name, Error: err.Error()}
	}
	var m NodeMetrics
	if err := json.Unmarshal(raw, &m); err != nil {
		return NodeMetric{NodeName: n.Name, Error: err.Error()}
	}
	return NodeMetric{NodeName: n.Name, Metrics: m}
}

// fetchInfo 拉取节点容量摘要（核数/内存/Docker 版本）。
func (o *Observer) fetchInfo(ctx context.Context, n *agentregistry.Node) (NodeInfo, error) {
	raw, err := o.call(ctx, n, agentprotocol.ActionSystemInfo)
	if err != nil {
		return NodeInfo{}, err
	}
	var info NodeInfo
	if err := json.Unmarshal(raw, &info); err != nil {
		return NodeInfo{}, err
	}
	return info, nil
}

// observeNode 采集受管容器列表。
func (o *Observer) observeNode(ctx context.Context, n *agentregistry.Node) ([]gwclient.Container, error) {
	raw, err := o.call(ctx, n, agentprotocol.ActionContainerList)
	if err != nil {
		return nil, err
	}
	var containers []gwclient.Container
	if err := json.Unmarshal(raw, &containers); err != nil {
		return nil, err
	}
	return containers, nil
}

// callTimeout 是单次只读命令的等待上限：节点卡死时不拖垮整轮观测。
const callTimeout = 20 * time.Second

// call 经 WS 任务通道下发一次只读命令。
func (o *Observer) call(ctx context.Context, n *agentregistry.Node, action string) (json.RawMessage, error) {
	cctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	return n.Call(cctx, action, nil)
}

// reportNode 上报节点注册/心跳，并把该节点上的容器作为实例上报。
func (o *Observer) reportNode(ctx context.Context, n *agentregistry.Node, containers []gwclient.Container) {
	if !o.gw.Enabled() {
		return
	}
	nodeID := n.ID()
	if nodeID == "" {
		return
	}
	if err := o.gw.HeartbeatNode(ctx, nodeID); err != nil {
		o.logger.Warn("node heartbeat to gateway failed", zap.String("node", n.Name), zap.Error(err))
	}

	sort.Slice(containers, func(i, j int) bool { return containers[i].InstanceID < containers[j].InstanceID })
	for _, ct := range containers {
		if ct.InstanceID == "" {
			continue
		}
		if ct.State != "running" {
			o.reportUnhealthy(ctx, n, nodeID, ct)
			continue
		}
		o.reportInstance(ctx, n, nodeID, ct)
	}
}

// reportUnhealthy 上报非运行容器为 unhealthy：让崩溃实例在 Gateway 侧以不健康形态可见。
func (o *Observer) reportUnhealthy(ctx context.Context, n *agentregistry.Node, nodeID string, ct gwclient.Container) {
	if err := o.gw.ReportHealth(ctx, ct.InstanceID, "unhealthy"); err == nil {
		return
	}
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
		o.logger.Warn("register crashed instance to gateway failed",
			zap.String("node", n.Name), zap.String("instance_id", ct.InstanceID), zap.Error(err))
		return
	}
	if err := o.gw.ReportHealth(ctx, ct.InstanceID, "unhealthy"); err != nil {
		o.logger.Warn("mark crashed instance unhealthy failed",
			zap.String("instance_id", ct.InstanceID), zap.Error(err))
	}
}

// reportInstance 上报单个容器对应的实例：优先心跳（已注册），失败则注册。
func (o *Observer) reportInstance(ctx context.Context, n *agentregistry.Node, nodeID string, ct gwclient.Container) {
	if err := o.gw.HeartbeatInstance(ctx, ct.InstanceID); err == nil {
		return
	}
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
