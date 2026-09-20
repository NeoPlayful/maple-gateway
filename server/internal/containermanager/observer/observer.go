// Package observer 周期采集各节点 Agent 上的容器实际态，并上报 Gateway。
//
// 职责（状态上行）：节点注册/心跳 → 容器发现 → 实例注册/心跳/注销。
// 容器 maple.instance_id 标签 = Gateway instances.id（同 UUID），保证一一对应。
//
// 采集经 WS 任务通道下发（container.list / system.info / node.metrics），走 Probe
// 通道只取数据、不产生任务记录；节点在线状态来自统一节点模型（有活跃 WS 会话才
// 尝试采集），不再依赖 HTTP 探测。
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
	"github.com/google/uuid"
	"go.uber.org/zap"
)

// applicationIDLabel 是 Compose 容器携带的应用标识标签（由应用注入，值即项目 ID）。
const applicationIDLabel = "maple.application_id"

// instanceIDNamespace 是 Compose 容器派生实例 ID 的 UUIDv5 命名空间（固定常量）。
// 同一容器名在任何进程、任何重启下都派生出同一实例 ID，保证跨 compose 重建稳定。
var instanceIDNamespace = uuid.MustParse("6f6f6b1e-9c2a-4b7e-9f1a-2b3c4d5e6f70")

// ServiceResolver 按应用 ID 解析其绑定的 Gateway 服务 ID，供 Compose 容器归入路由。
type ServiceResolver interface {
	ServiceForApplication(appID string) (serviceID string, ok bool)
}

// deriveInstanceID 为无 instance_id 的 Compose 容器派生稳定实例 ID：以容器名为唯一输入
// 做 UUIDv5，使其像单容器一样注册为实例，并在管理端与运行时容器列表一一对应。
func deriveInstanceID(name string) string {
	return uuid.NewSHA1(instanceIDNamespace, []byte(name)).String()
}

// NodeInfo 是节点容量摘要（对应 Agent docker.NodeInfo）。
type NodeInfo struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	CPUs          int    `json:"cpus"`
	MemoryBytes   int64  `json:"memory_bytes"`
	Containers    int    `json:"containers"`
	DockerVersion string `json:"docker_version"`
	DockerAPIVer  string `json:"docker_api_version"`
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
	LoadAvg1    float64 `json:"load_avg_1"`
	UptimeSec   int64   `json:"uptime_sec"`
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
	// apps 按应用 ID 解析绑定服务，供 Compose 容器归入路由（可空：nil 时 Compose
	// 容器因无服务归属而不上报）。
	apps ServiceResolver

	// kick 用于事件驱动的即时观测：docker 事件到达时触发一次 tick，
	// 让容器/部署状态尽快收敛，而不必等待下一个轮询周期。
	kick chan struct{}

	mu          sync.Mutex
	lastReport  time.Time
	lastErr     error
	containerCt int
	nodeUpCt    int
	snapshot    map[string]ObservedContainer
	// byContainerID 是按容器 ID 索引的全量受管容器（含无 instance_id 的 Compose 容器），
	// 供容器列表展示与按容器 ID 的人工操作；对账仍只读 instance_id 索引的 snapshot。
	byContainerID map[string]ObservedContainer
	// reported 是本轮实际上报（注册/心跳）过的实例：instance_id → 节点名。仅这些实例
	// 在消失时才需向 Gateway 注销；无服务归属的 Compose 容器从未注册，不入此集合。
	reported map[string]string
	metrics       map[string]NodeMetric
	info          map[string]NodeInfo
	errors        []RuntimeError
	events        []DockerEvent
	eventKeys     map[string]struct{} // 事件去重键集合

	// observeHealth 记录各节点连续观测失败次数与退避窗口（由 mu 保护）。
	observeHealth map[string]*nodeHealth
}

// nodeHealth 是单个节点的观测健康态：一旦失败即进入退避窗口，
// 连续失败则窗口逐次拉长；窗口内跳过该节点的探测，避免坏节点
// （如 Docker 引擎无响应）每轮空转并刷屏。
type nodeHealth struct {
	fails   int
	retryAt time.Time
}

// 观测退避参数：连续失败 n 次的退避时长为 base<<(n-1)，上限 max。
// 首个退避即超出轮询周期，坏节点很快从"每轮探测"降到"偶发重试"。
const (
	observeBackoffBase = 30 * time.Second
	observeBackoffMax  = 5 * time.Minute
)

// noteObserveFailure 记录一次观测失败、推进退避窗口，并返回累计失败次数。
func (o *Observer) noteObserveFailure(name string, now time.Time) int {
	o.mu.Lock()
	defer o.mu.Unlock()
	h := o.observeHealth[name]
	if h == nil {
		h = &nodeHealth{}
		o.observeHealth[name] = h
	}
	h.fails++
	backoff := observeBackoffBase << (h.fails - 1)
	if backoff <= 0 || backoff > observeBackoffMax {
		backoff = observeBackoffMax
	}
	h.retryAt = now.Add(backoff)
	return h.fails
}

// noteObserveSuccess 清除节点的失败计数与退避窗口。
func (o *Observer) noteObserveSuccess(name string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	delete(o.observeHealth, name)
}

// inObserveBackoff 报告节点当前是否处于退避窗口内（应跳过本轮探测）。
func (o *Observer) inObserveBackoff(name string, now time.Time) bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	h := o.observeHealth[name]
	return h != nil && now.Before(h.retryAt)
}

// degradedCountLocked 报告处于退避窗口内的节点数；调用方须持有 o.mu。
func (o *Observer) degradedCountLocked(now time.Time) int {
	n := 0
	for _, h := range o.observeHealth {
		if now.Before(h.retryAt) {
			n++
		}
	}
	return n
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
		registry:      registry,
		gw:            gw,
		interval:      interval,
		logger:        logger,
		kick:          make(chan struct{}, 1),
		snapshot:      map[string]ObservedContainer{},
		byContainerID: map[string]ObservedContainer{},
		reported:      map[string]string{},
		metrics:       map[string]NodeMetric{},
		info:          map[string]NodeInfo{},
		eventKeys:     map[string]struct{}{},
		observeHealth: map[string]*nodeHealth{},
	}
}

// WithServiceResolver 注入应用→服务解析器（Compose 容器据此归入路由）。
func (o *Observer) WithServiceResolver(r ServiceResolver) *Observer {
	o.apps = r
	return o
}

// serviceFor 解析容器归属的服务 ID：优先取容器标签（声明式容器自带），
// 为空则按应用 ID 查其绑定服务（Compose 容器）。
func (o *Observer) serviceFor(ct gwclient.Container) string {
	if sid := ct.Labels["maple.service_id"]; sid != "" {
		return sid
	}
	if o.apps == nil {
		return ""
	}
	appID := ct.Labels[applicationIDLabel]
	if appID == "" {
		return ""
	}
	if sid, ok := o.apps.ServiceForApplication(appID); ok {
		return sid
	}
	return ""
}

// Run 启动周期观测循环，直到 ctx 取消。docker 事件到达时经 kick 触发即时观测。
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
		case <-o.kick:
			o.tick(ctx)
		}
	}
}

// maxEvents 是保留的事件条数上限（超出即丢弃最旧的）。
const maxEvents = 500

// OnDockerEvent 接收一条 Agent 上行的 Docker 事件：去重、入库并触发即时观测。
func (o *Observer) OnDockerEvent(nodeID string, p agentprotocol.DockerEventPayload) {
	key := eventKey(nodeID, p)
	o.mu.Lock()
	if _, dup := o.eventKeys[key]; dup {
		o.mu.Unlock()
		return
	}
	o.eventKeys[key] = struct{}{}
	// 事件按时间单调递增，新事件追加即有序；超上限则丢弃最旧并清理其去重键。
	o.events = append(o.events, DockerEvent{
		NodeID:      nodeID,
		Action:      p.Action,
		ContainerID: p.ContainerID,
		InstanceID:  p.InstanceID,
		Image:       p.Image,
		ExitCode:    p.ExitCode,
		At:          p.Time,
	})
	if len(o.events) > maxEvents {
		drop := o.events[0]
		delete(o.eventKeys, eventKey(nodeID, agentprotocol.DockerEventPayload{
			Action: drop.Action, ContainerID: drop.ContainerID,
			InstanceID: drop.InstanceID, Time: drop.At,
		}))
		o.events = o.events[1:]
	}
	o.mu.Unlock()
	o.kickObserve()
}

// kickObserve 非阻塞地请求一次即时观测。
func (o *Observer) kickObserve() {
	select {
	case o.kick <- struct{}{}:
	default:
	}
}

// eventKey 生成事件去重键（节点 + 容器 + 动作 + 时间）。
func eventKey(nodeID string, p agentprotocol.DockerEventPayload) string {
	return nodeID + "|" + p.ContainerID + "|" + p.Action + "|" + time.Unix(p.Time, 0).Format(time.RFC3339Nano) + "|" + p.InstanceID
}

// DockerEvent 是 CM 记录的一条受管容器事件。
type DockerEvent struct {
	NodeID      string `json:"node_id"`
	Action      string `json:"action"`
	ContainerID string `json:"container_id,omitempty"`
	InstanceID  string `json:"instance_id,omitempty"`
	Image       string `json:"image,omitempty"`
	ExitCode    int    `json:"exit_code,omitempty"`
	At          int64  `json:"at"`
}

// tick 执行一轮观测与上报。
func (o *Observer) tick(ctx context.Context) {
	nodes := o.registry.All()
	upCt, ct := 0, 0
	var firstErr error
	snap := make(map[string]ObservedContainer)
	byCID := make(map[string]ObservedContainer)
	metrics := make(map[string]NodeMetric, len(nodes))
	infos := make(map[string]NodeInfo, len(nodes))
	var runtimeErrs []RuntimeError
	observedNodes := map[string]bool{}
	// reported 汇总本轮实际进入实例视图（有服务归属）的实例：仅这些实例在消失时才需注销。
	reported := map[string]string{}
	now := time.Now()
	for _, n := range nodes {
		// 仅在节点有活跃 WS 会话、且已认领 Gateway 身份时才观测。
		if !n.Online() || n.ID() == "" {
			continue
		}
		// 连续失败的节点处于退避窗口内：本轮跳过探测，避免坏节点每轮空转。
		if o.inObserveBackoff(n.Name, now) {
			continue
		}
		// 三项只读探测并发下发：单节点无响应时本轮等待以单次 callTimeout 封顶，
		// 而非三项串行累加（3×callTimeout），避免一个坏节点拖垮整轮观测。
		var (
			metric     NodeMetric
			info       NodeInfo
			infoOK     bool
			containers []gwclient.Container
			obsErr     error
		)
		var wg sync.WaitGroup
		wg.Add(3)
		go func() { defer wg.Done(); metric = o.collectMetrics(ctx, n) }()
		go func() {
			defer wg.Done()
			if in, err := o.fetchInfo(ctx, n); err == nil {
				info, infoOK = in, true
			}
		}()
		go func() { defer wg.Done(); containers, obsErr = o.observeNode(ctx, n) }()
		wg.Wait()

		metrics[n.Name] = metric
		if infoOK {
			infos[n.Name] = info
		}
		if obsErr != nil {
			if firstErr == nil {
				firstErr = obsErr
			}
			fails := o.noteObserveFailure(n.Name, time.Now())
			o.logger.Warn("observe node failed", zap.String("node", n.Name),
				zap.Int("consecutive", fails), zap.Error(obsErr))
			continue
		}
		o.noteObserveSuccess(n.Name)
		upCt++
		ct += len(containers)
		observedNodes[n.Name] = true
		for i := range containers {
			c := containers[i]
			// Compose 容器无 instance_id 标签：以容器名派生稳定实例 ID，使其像单容器
			// 一样进入实例视图（与运行时容器列表一一对应）。派生仅按标签，不改容器本身。
			if c.InstanceID == "" && c.Name != "" && c.Labels[applicationIDLabel] != "" {
				c.InstanceID = deriveInstanceID(c.Name)
				containers[i].InstanceID = c.InstanceID
			}
			if c.ID != "" {
				byCID[c.ID] = ObservedContainer{NodeName: n.Name, InstanceID: c.InstanceID, Container: c}
			}
			// snapshot 收全部有实例 ID 的容器（含未绑定服务的 Compose 容器）：它是人工操作
			// （stats/logs/启停）定位容器的索引，须覆盖运行时列表里的每一个容器，否则按
			// 派生实例 ID 发起的操作会因定位不到而失败。
			if c.InstanceID != "" {
				snap[c.InstanceID] = ObservedContainer{NodeName: n.Name, InstanceID: c.InstanceID, Container: c}
			}
			// 仅"有服务归属"的容器真正进入实例视图；记入 reported，供消失注销按需收敛。
			if c.InstanceID != "" && o.serviceFor(c) != "" {
				reported[c.InstanceID] = n.Name
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
	prevReported := o.reported
	o.nodeUpCt = upCt
	o.containerCt = ct
	o.snapshot = snap
	o.byContainerID = byCID
	o.reported = reported
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

	// 消失注销：上一轮确实注册过、本轮未见于"已成功观测节点"上的实例 → 通知 Gateway 注销。
	// 以 reported（有服务归属）而非 snapshot 为基准：无服务归属的 Compose 容器从未注册，
	// 其消失不应触发注销告警。
	for iid, nodeName := range prevReported {
		if !observedNodes[nodeName] {
			continue
		}
		if _, still := reported[iid]; still {
			continue
		}
		if o.gw.Enabled() {
			if err := o.gw.DeleteInstance(ctx, iid); err != nil {
				o.logger.Warn("delete vanished instance via gateway failed",
					zap.String("instance_id", iid), zap.Error(err))
			} else {
				o.logger.Info("vanished instance deregistered",
					zap.String("instance_id", iid), zap.String("node", nodeName))
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

// Containers 返回最近一轮观测到的全部受管容器（含无 instance_id 的 Compose 容器），
// 供容器列表展示与人工操作读取。
func (o *Observer) Containers() map[string]ObservedContainer {
	o.mu.Lock()
	defer o.mu.Unlock()
	out := make(map[string]ObservedContainer, len(o.byContainerID))
	for k, v := range o.byContainerID {
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

// Events 返回最近记录的 Docker 事件（按时间倒序，最新在前）。
func (o *Observer) Events() []DockerEvent {
	o.mu.Lock()
	defer o.mu.Unlock()
	out := make([]DockerEvent, len(o.events))
	copy(out, o.events)
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

// call 经 WS 任务通道下发一次只读探测。走 Probe 通道：仍取回探测数据，
// 但不产生任务记录，避免每轮观测的三项探测在任务列表里累积。
func (o *Observer) call(ctx context.Context, n *agentregistry.Node, action string) (json.RawMessage, error) {
	cctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	return n.Probe(cctx, action, nil)
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
		// 无服务归属的容器不进实例视图：它无法参与路由，注册也会被 Gateway 拒（service_id 必填）。
		if o.serviceFor(ct) == "" {
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
		ServiceID:    o.serviceFor(ct),
		DeploymentID: ct.Labels["maple.deployment_id"],
		VersionID:    ct.Labels["maple.version_id"],
		ProjectID:    ct.Labels["maple.project_id"],
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
	// 心跳带上项目归属与宿主端口：容器创建时端口映射尚未就绪（读到 0），
	// 且早期实例可能漏填 project_id，就绪后经此补正。
	if err := o.gw.HeartbeatInstance(ctx, ct.InstanceID, nodeID, ct.Labels["maple.project_id"], ct.HostPort); err == nil {
		return
	}
	rep := gwclient.InstanceReport{
		ID:           ct.InstanceID,
		ServiceID:    o.serviceFor(ct),
		DeploymentID: ct.Labels["maple.deployment_id"],
		VersionID:    ct.Labels["maple.version_id"],
		ProjectID:    ct.Labels["maple.project_id"],
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
	Degraded     int       `json:"degraded"` // 处于观测退避窗口内的节点数
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
		Degraded:     o.degradedCountLocked(time.Now()),
		LastReportAt: o.lastReport,
	}
	if o.lastErr != nil {
		s.LastError = o.lastErr.Error()
	}
	return s
}
