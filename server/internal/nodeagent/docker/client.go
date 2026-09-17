// Package docker 封装本机 Docker Engine 客户端，供 Node Agent 创建/启停/删除容器。
//
// Node Agent 是平台内唯一直连 Docker Engine 的组件；本包只操作带受管标签的容器，
// 避免误动宿主上的其他容器。标签规范：
//
//	maple.managed        = "true"     受管标记（必需，缺标签即非受管）
//	maple.instance_id    = <uuid>     与 Gateway instances.id 一一对应
//	maple.service_id     = <uuid>
//	maple.deployment_id  = <uuid>
//	maple.version_id     = <uuid>
package docker

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/events"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/stdcopy"
	"github.com/docker/go-connections/nat"
)

// 受管容器标签键。
const (
	LabelManaged      = "maple.managed"
	LabelInstanceID   = "maple.instance_id"
	LabelServiceID    = "maple.service_id"
	LabelDeploymentID = "maple.deployment_id"
	LabelVersionID    = "maple.version_id"
)

// Client 是 Docker Engine 客户端封装。
type Client struct {
	cli          *client.Client
	managedLabel string
	// dataRoot 是本节点数据根目录（宿主机绝对路径）。容器绑定挂载的宿主路径一律
	// 由本节点用它拼出，越界无法发生；为空则节点不承载任何带挂载的容器。
	dataRoot string

	// lastSample 缓存各容器上一次采集的累计计数器与时刻，供两次采样差分算速率。
	// 统计为按需拉取（详情面板打开期间），首次采样无前值，速率返回 0。
	statsMu    sync.Mutex
	lastSample map[string]statSample

	// imageMu 保护 imageRefs。镜像引用一经创建即不可变，故按「容器 ID + 镜像 ID」
	// 缓存，命中即免去逐容器 Inspect；容器被替换或换镜像时键变化，自动重新解析。
	imageMu   sync.Mutex
	imageRefs map[string]string
}

// imageRefKey 是镜像引用缓存的键：容器与镜像 ID 都不变时引用必然不变。
func imageRefKey(containerID, imageID string) string { return containerID + "|" + imageID }

// statSample 是一次采样的累计计数器（网络收发、块设备读写）与时刻。
type statSample struct {
	at        time.Time
	rx, tx    uint64
	blkRead   uint64
	blkWrite  uint64
}

// New 构造。managedLabel 为受管标签键；host 为空则用 SDK 平台默认端点
// （Linux: unix socket；Windows: npipe）。dataRoot 为本节点数据根目录（可空）。
func New(host, managedLabel, dataRoot string) (*Client, error) {
	opts := []client.Opt{client.WithAPIVersionNegotiation()}
	if host != "" {
		opts = append(opts, client.WithHost(host))
	} else {
		opts = append(opts, client.FromEnv)
	}
	cli, err := client.NewClientWithOpts(opts...)
	if err != nil {
		return nil, fmt.Errorf("docker client: %w", err)
	}
	return &Client{
		cli:          cli,
		managedLabel: managedLabel,
		dataRoot:     normalizeRoot(dataRoot),
		lastSample:   map[string]statSample{},
		imageRefs:    map[string]string{},
	}, nil
}

// Close 释放底层连接。
func (c *Client) Close() error { return c.cli.Close() }

// Ping 探测 Docker 引擎是否响应（连接建立后的存活检测）。
func (c *Client) Ping(ctx context.Context) error {
	if _, err := c.cli.Ping(ctx); err != nil {
		return fmt.Errorf("docker ping: %w", err)
	}
	return nil
}

// CreateSpec 是创建一个容器所需的规格。
type CreateSpec struct {
	InstanceID   string            `json:"instance_id"`
	ServiceID    string            `json:"service_id"`
	DeploymentID string            `json:"deployment_id"`
	VersionID    string            `json:"version_id"`
	Image        string            `json:"image"`
	Port         int               `json:"port"`      // 容器监听端口
	HostPort     int               `json:"host_port"` // 本机映射端口（0 = 由 Docker 动态分配）
	Env          map[string]string `json:"env"`
	// 可选 CPU/内存限制：CPUs（如 "0.5"）暂不注入，Memory 支持 "256m"/"1g"。
	Memory     string   `json:"memory"`
	Command    []string `json:"command"`
	HealthPath string   `json:"health_path"`
	// Mounts 是绑定挂载项：Path 相对本节点数据根，形如 <租户>/<模板>/<项目>/...。
	Mounts []Mount `json:"mounts,omitempty"`
}

// Mount 是一次绑定挂载：把数据根下的子目录映射进容器。
// 控制面只给出相对数据根的 Path，宿主绝对路径由本节点用自身数据根拼出——
// 数据根只存在于节点本地配置，越界因此无从发生。
type Mount struct {
	// Path 相对数据根的子路径（如 "t1/tpl/p1/db"）；不含上跳段。
	Path string `json:"path"`
	// Target 容器内挂载点（绝对路径）。
	Target string `json:"target"`
	// ReadOnly 为真则以只读方式挂载。
	ReadOnly bool `json:"read_only,omitempty"`
}

// Container 是受管容器的观测视图。
type Container struct {
	ID         string            `json:"id"`
	Name       string            `json:"name"`
	Image      string            `json:"image"`
	State      string            `json:"state"`  // created / running / exited / …
	Status     string            `json:"status"` // 人类可读状态
	Labels     map[string]string `json:"labels"`
	InstanceID    string            `json:"instance_id"`
	HostPort      int               `json:"host_port"`      // 本机映射端口（从端口映射解析）
	ContainerPort int               `json:"container_port"` // 容器内部监听端口（同一条端口映射的 PrivatePort）
	IP            string            `json:"ip"`             // 容器网络 IP（按网络名排序取首个非空）
	// 退出信息：非 running 容器的诊断线索（exited/dead 时填充）。
	ExitCode   int    `json:"exit_code"`
	OOMKilled  bool   `json:"oom_killed"`
	FinishedAt string `json:"finished_at,omitempty"`
}

// NodeInfo 是节点资源与容量摘要。
type NodeInfo struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	CPUs          int    `json:"cpus"`
	MemoryBytes   int64  `json:"memory_bytes"`
	Containers    int    `json:"containers"` // 运行中的受管容器数
	DockerVersion string `json:"docker_version"`
	DockerAPIVer  string `json:"docker_api_version"`
}

// DiskUsage 是 Docker 引擎空间占用摘要。
type DiskUsage struct {
	LayersSize int64 `json:"layers_size"` // 镜像层总大小（字节）
	Images     int   `json:"images"`
	Containers int   `json:"containers"`
	Volumes    int   `json:"volumes"`
}

// ContainerStats 是单个容器的资源用量视图（按需采集，供详情面板）。
// 速率类字段（*_bps）由两次采样对累计计数器做差得出；首次采样无前值时为 0。
type ContainerStats struct {
	ContainerID string `json:"container_id"`

	CPUPercent float64 `json:"cpu_percent"` // CPU 使用率 %（按在线核数归一）

	MemUsage   int64   `json:"mem_usage"`   // 已用内存（字节，已扣除文件缓存）
	MemLimit   int64   `json:"mem_limit"`   // 内存上限（字节，0 表示无限制）
	MemPercent float64 `json:"mem_percent"` // 内存使用率 %

	// 网络：累计收发总量与差分速率。
	NetRxBytes uint64  `json:"net_rx_bytes"`
	NetTxBytes uint64  `json:"net_tx_bytes"`
	NetRxBps   float64 `json:"net_rx_bps"`
	NetTxBps   float64 `json:"net_tx_bps"`

	// 块设备 IO：累计读写总量与差分速率。
	BlkReadBytes  uint64  `json:"blk_read_bytes"`
	BlkWriteBytes uint64  `json:"blk_write_bytes"`
	BlkReadBps    float64 `json:"blk_read_bps"`
	BlkWriteBps   float64 `json:"blk_write_bps"`

	PidsCurrent uint64 `json:"pids_current"` // 当前进程/线程数
}

// managedFilter 返回"仅受管容器"的过滤参数。
func (c *Client) managedFilter() filters.Args {
	f := filters.NewArgs()
	f.Add("label", c.managedLabel+"=true")
	return f
}

// Pull 拉取镜像。allowed 非空时按前缀校验，未命中返回错误。
func (c *Client) Pull(ctx context.Context, ref string, allowed []string) error {
	if !imageAllowed(ref, allowed) {
		return fmt.Errorf("image %q not in allowed list", ref)
	}
	rc, err := c.cli.ImagePull(ctx, ref, image.PullOptions{})
	if err != nil {
		return fmt.Errorf("pull image: %w", err)
	}
	defer func() { _ = rc.Close() }()
	// 必须读完响应流，否则拉取可能未真正完成。
	_, _ = io.Copy(io.Discard, rc)
	return nil
}

// imageAllowed 判断镜像是否命中允许清单（前缀匹配）；清单为空表示不限制。
func imageAllowed(ref string, allowed []string) bool {
	if len(allowed) == 0 {
		return true
	}
	for _, p := range allowed {
		if p != "" && strings.HasPrefix(ref, p) {
			return true
		}
	}
	return false
}

// Ensure 幂等创建并启动容器：同 instance_id 的受管容器已存在则直接返回。
// 返回创建/命中的容器 ID 与实际映射的本机端口。
func (c *Client) Ensure(ctx context.Context, spec CreateSpec, allowedImages []string) (string, int, error) {
	if existing, ok, err := c.FindByInstance(ctx, spec.InstanceID); err != nil {
		return "", 0, err
	} else if ok {
		return existing.ID, existing.HostPort, nil
	}
	if err := c.Pull(ctx, spec.Image, allowedImages); err != nil {
		return "", 0, err
	}

	env := make([]string, 0, len(spec.Env))
	for k, v := range spec.Env {
		env = append(env, k+"="+v)
	}
	sort.Strings(env)

	labels := map[string]string{
		c.managedLabel:  "true",
		LabelInstanceID: spec.InstanceID,
	}
	if spec.ServiceID != "" {
		labels[LabelServiceID] = spec.ServiceID
	}
	if spec.DeploymentID != "" {
		labels[LabelDeploymentID] = spec.DeploymentID
	}
	if spec.VersionID != "" {
		labels[LabelVersionID] = spec.VersionID
	}

	cfg := &container.Config{
		Image:  spec.Image,
		Env:    env,
		Labels: labels,
		Cmd:    spec.Command,
	}
	hostCfg := &container.HostConfig{}
	// 绑定挂载：规格只给相对数据根的子路径，本节点拼出宿主绝对路径并做越界校验。
	binds, err := c.buildBinds(spec.Mounts)
	if err != nil {
		return "", 0, err
	}
	hostCfg.Binds = binds
	if spec.Port > 0 {
		containerPort := nat.Port(fmt.Sprintf("%d/tcp", spec.Port))
		cfg.ExposedPorts = nat.PortSet{containerPort: struct{}{}}
		hostPort := ""
		if spec.HostPort > 0 {
			hostPort = fmt.Sprintf("%d", spec.HostPort)
		}
		hostCfg.PortBindings = nat.PortMap{
			containerPort: []nat.PortBinding{{HostIP: "", HostPort: hostPort}},
		}
	}
	if spec.Memory != "" {
		hostCfg.Resources.Memory = parseMemory(spec.Memory)
	}

	created, err := c.cli.ContainerCreate(ctx, cfg, hostCfg, nil, nil, "maple-"+shortID(spec.InstanceID))
	if err != nil {
		return "", 0, fmt.Errorf("create container: %w", err)
	}
	if err := c.cli.ContainerStart(ctx, created.ID, container.StartOptions{}); err != nil {
		// 启动失败则清理，避免留下半成品。
		_ = c.cli.ContainerRemove(ctx, created.ID, container.RemoveOptions{Force: true})
		return "", 0, fmt.Errorf("start container: %w", err)
	}

	// 回读实际映射端口（动态分配时 Docker 才给出最终端口）。
	// 端口绑定在启动后由引擎异步填充，首次 Inspect 可能读不到，短暂重试。
	hostPort := spec.HostPort
	if spec.Port > 0 {
		for i := 0; i < 10; i++ {
			insp, err := c.cli.ContainerInspect(ctx, created.ID)
			if err == nil {
				if p := hostPortFromInspect(insp, spec.Port); p > 0 {
					hostPort = p
					break
				}
			}
			select {
			case <-ctx.Done():
				return created.ID, hostPort, nil
			case <-time.After(150 * time.Millisecond):
			}
		}
	}
	return created.ID, hostPort, nil
}

// Start 启动容器。
func (c *Client) Start(ctx context.Context, id string) error {
	if err := c.cli.ContainerStart(ctx, id, container.StartOptions{}); err != nil {
		return fmt.Errorf("start container: %w", err)
	}
	return nil
}

// Stop 优雅停止容器（timeout 秒后强杀）。
func (c *Client) Stop(ctx context.Context, id string, timeout time.Duration) error {
	secs := int(timeout.Seconds())
	opts := container.StopOptions{Timeout: &secs}
	if err := c.cli.ContainerStop(ctx, id, opts); err != nil {
		return fmt.Errorf("stop container: %w", err)
	}
	return nil
}

// Remove 删除容器（force=true 时强制删除运行中的容器）。
func (c *Client) Remove(ctx context.Context, id string, force bool) error {
	opts := container.RemoveOptions{Force: force, RemoveVolumes: false}
	if err := c.cli.ContainerRemove(ctx, id, opts); err != nil {
		return fmt.Errorf("remove container: %w", err)
	}
	return nil
}

// Restart 重启容器（先优雅停止再启动，timeout 秒后强杀）。
func (c *Client) Restart(ctx context.Context, id string, timeout time.Duration) error {
	secs := int(timeout.Seconds())
	opts := container.StopOptions{Timeout: &secs}
	if err := c.cli.ContainerRestart(ctx, id, opts); err != nil {
		return fmt.Errorf("restart container: %w", err)
	}
	return nil
}

// Logs 读取容器最近 tail 行日志，合并 stdout 与 stderr（按时间戳前缀可选）。
func (c *Client) Logs(ctx context.Context, id string, tail int) (string, error) {
	if tail <= 0 || tail > 5000 {
		tail = 200
	}
	opts := container.LogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Timestamps: true,
		Tail:       strconv.Itoa(tail),
	}
	rc, err := c.cli.ContainerLogs(ctx, id, opts)
	if err != nil {
		return "", fmt.Errorf("container logs: %w", err)
	}
	defer func() { _ = rc.Close() }()
	// 非 TTY 容器：stdout/stderr 以帧复用，需 demux 后按序拼接。
	var out, errBuf bytes.Buffer
	if _, err := stdcopy.StdCopy(&out, &errBuf, rc); err != nil {
		return "", fmt.Errorf("read container logs: %w", err)
	}
	combined := out.String()
	if errBuf.Len() > 0 {
		if combined != "" && !strings.HasSuffix(combined, "\n") {
			combined += "\n"
		}
		combined += errBuf.String()
	}
	return combined, nil
}

// FollowLogs 持续跟随容器日志，每读到一段即回调 emit。
// 非 TTY 容器的 stdout/stderr 以帧复用，需逐帧 demux；follow 会保持连接直到
// ctx 取消或容器结束。tail<=0 时取默认 200 行历史。
func (c *Client) FollowLogs(ctx context.Context, id string, tail int, emit func(chunk string) error) error {
	if tail <= 0 || tail > 5000 {
		tail = 200
	}
	opts := container.LogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Timestamps: true,
		Follow:     true,
		Tail:       strconv.Itoa(tail),
	}
	rc, err := c.cli.ContainerLogs(ctx, id, opts)
	if err != nil {
		return fmt.Errorf("container logs: %w", err)
	}
	defer func() { _ = rc.Close() }()

	header := make([]byte, 8)
	for {
		if _, err := io.ReadFull(rc, header); err != nil {
			if err == io.EOF || err == io.ErrUnexpectedEOF {
				return nil // 流自然结束
			}
			if ctx.Err() != nil {
				return nil // 上下文取消视为正常收尾
			}
			return fmt.Errorf("read log frame: %w", err)
		}
		size := int(header[4])<<24 | int(header[5])<<16 | int(header[6])<<8 | int(header[7])
		if size <= 0 {
			continue
		}
		payload := make([]byte, size)
		if _, err := io.ReadFull(rc, payload); err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return fmt.Errorf("read log payload: %w", err)
		}
		if err := emit(string(payload)); err != nil {
			return err // 回调侧要求终止（如写失败）
		}
	}
}

// DockerEvent 是一条受管容器相关的 Docker 事件（已映射为平台视图）。
type DockerEvent struct {
	Action      string
	ContainerID string
	InstanceID  string
	Image       string
	ExitCode    int
	Time        int64 // Unix 秒
}

// WatchEvents 订阅 Docker 容器事件并逐条回调 emit，直到 ctx 取消。
// 只上报带受管标签的容器事件（非受管容器一律忽略）。
func (c *Client) WatchEvents(ctx context.Context, emit func(DockerEvent)) error {
	f := filters.NewArgs()
	f.Add("type", string(events.ContainerEventType))
	msgCh, errCh := c.cli.Events(ctx, events.ListOptions{Filters: f})

	for {
		select {
		case <-ctx.Done():
			return nil
		case err := <-errCh:
			if ctx.Err() != nil {
				return nil
			}
			return fmt.Errorf("watch docker events: %w", err)
		case msg := <-msgCh:
			ev, ok := c.fromMessage(msg)
			if ok {
				emit(ev)
			}
		}
	}
}

// fromMessage 把一条 Docker 事件映射为受管事件；非受管容器返回 false。
func (c *Client) fromMessage(msg events.Message) (DockerEvent, bool) {
	if msg.Actor.Attributes[c.managedLabel] != "true" {
		return DockerEvent{}, false
	}
	ev := DockerEvent{
		Action:      string(msg.Action),
		ContainerID: msg.Actor.ID,
		InstanceID:  msg.Actor.Attributes[LabelInstanceID],
		Image:       msg.Actor.Attributes["image"],
		Time:        msg.Time,
	}
	// die 事件带退出码：exitCode 为字符串属性。
	if msg.Action == events.ActionDie {
		if code, err := strconv.Atoi(msg.Actor.Attributes["exitCode"]); err == nil {
			ev.ExitCode = code
		}
	}
	return ev, true
}

// DiskUsage 返回 Docker 引擎的空间占用摘要。
func (c *Client) DiskUsage(ctx context.Context) (DiskUsage, error) {
	du, err := c.cli.DiskUsage(ctx, types.DiskUsageOptions{})
	if err != nil {
		return DiskUsage{}, fmt.Errorf("docker disk usage: %w", err)
	}
	return DiskUsage{
		LayersSize: du.LayersSize,
		Images:     len(du.Images),
		Containers: len(du.Containers),
		Volumes:    len(du.Volumes),
	}, nil
}

// Stats 采集单个容器的资源用量（CPU/内存/网络/磁盘 IO）。id 可为容器 ID 或 instance_id。
// 网络与块 IO 的速率由本方法维护的上一次采样差分得出，故面板应周期性调用以维持采样点。
func (c *Client) Stats(ctx context.Context, id string) (ContainerStats, error) {
	real, err := c.resolve(ctx, id)
	if err != nil {
		return ContainerStats{}, err
	}
	resp, err := c.cli.ContainerStatsOneShot(ctx, real)
	if err != nil {
		return ContainerStats{}, fmt.Errorf("container stats: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	var raw container.StatsResponse
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return ContainerStats{}, fmt.Errorf("decode container stats: %w", err)
	}
	return c.toContainerStats(real, raw), nil
}

// resolve 把传入标识解析为真实容器 ID：优先按 instance_id 标签匹配受管容器，
// 未命中则回退按容器 ID/名称直查（仍限受管容器）。
func (c *Client) resolve(ctx context.Context, id string) (string, error) {
	if looksLikeUUID(id) {
		if ct, ok, err := c.FindByInstance(ctx, id); err != nil {
			return "", err
		} else if ok {
			return ct.ID, nil
		}
	}
	return id, nil
}

// looksLikeUUID 粗判是否 UUID 形态（36 字符 4 连字符），用于选择解析路径。
func looksLikeUUID(s string) bool {
	if len(s) != 36 {
		return false
	}
	for i, r := range s {
		if i == 8 || i == 13 || i == 18 || i == 23 {
			if r != '-' {
				return false
			}
			continue
		}
		if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F')) {
			return false
		}
	}
	return strings.Count(s, "-") == 4
}

// toContainerStats 把 Docker 原始 StatsResponse 映射为用量视图并计算差分速率。
func (c *Client) toContainerStats(containerID string, raw container.StatsResponse) ContainerStats {
	st := ContainerStats{ContainerID: containerID}

	// CPU%：本周期 CPU 增量 / 系统 CPU 增量 × 在线核数 × 100。
	// 单次 OneShot 自带 precpu，可直接得出；系统增量缺失时退化为 0。
	cpuDelta := float64(raw.CPUStats.CPUUsage.TotalUsage - raw.PreCPUStats.CPUUsage.TotalUsage)
	sysDelta := float64(raw.CPUStats.SystemUsage - raw.PreCPUStats.SystemUsage)
	onlineCPUs := float64(raw.CPUStats.OnlineCPUs)
	if onlineCPUs == 0 {
		onlineCPUs = float64(len(raw.CPUStats.CPUUsage.PercpuUsage))
	}
	if sysDelta > 0 && cpuDelta > 0 {
		st.CPUPercent = cpuDelta / sysDelta * onlineCPUs * 100
	}

	// 内存：usage 含文件缓存，扣除 inactive_file 更贴近实际占用（与 docker stats 口径一致）。
	usage := raw.MemoryStats.Usage
	if v, ok := raw.MemoryStats.Stats["total_inactive_file"]; ok && v < usage {
		usage -= v
	} else if v, ok := raw.MemoryStats.Stats["inactive_file"]; ok && v < usage {
		usage -= v
	}
	st.MemUsage = int64(usage)
	st.MemLimit = int64(raw.MemoryStats.Limit)
	if st.MemLimit > 0 {
		st.MemPercent = float64(st.MemUsage) / float64(st.MemLimit) * 100
	}

	// 网络：按网卡累加收发字节。
	for _, n := range raw.Networks {
		st.NetRxBytes += n.RxBytes
		st.NetTxBytes += n.TxBytes
	}
	st.BlkReadBytes, st.BlkWriteBytes = blkioBytes(raw.BlkioStats)
	st.PidsCurrent = raw.PidsStats.Current

	// 差分速率：与上一次采样（同容器）做差；首次采样无前值则速率为 0。
	c.statsMu.Lock()
	prev, ok := c.lastSample[containerID]
	c.lastSample[containerID] = statSample{
		at: time.Now(), rx: st.NetRxBytes, tx: st.NetTxBytes,
		blkRead: st.BlkReadBytes, blkWrite: st.BlkWriteBytes,
	}
	c.statsMu.Unlock()
	if ok {
		if secs := time.Since(prev.at).Seconds(); secs > 0 {
			st.NetRxBps = rate(prev.rx, st.NetRxBytes, secs)
			st.NetTxBps = rate(prev.tx, st.NetTxBytes, secs)
			st.BlkReadBps = rate(prev.blkRead, st.BlkReadBytes, secs)
			st.BlkWriteBps = rate(prev.blkWrite, st.BlkWriteBytes, secs)
		}
	}
	return st
}

// blkioBytes 汇总块设备读写累计字节（按 Op 分类，兼容 read/write 与 Read/Write 大小写）。
func blkioBytes(b container.BlkioStats) (read, write uint64) {
	for _, e := range b.IoServiceBytesRecursive {
		switch strings.ToLower(e.Op) {
		case "read":
			read += e.Value
		case "write":
			write += e.Value
		}
	}
	return read, write
}

// rate 由两次累计值之差除以间隔秒数得出速率；计数器回绕（负增量）时返回 0。
func rate(prev, cur uint64, secs float64) float64 {
	if cur < prev {
		return 0
	}
	return float64(cur-prev) / secs
}

// FindByInstance 按 instance_id 标签查找受管容器。
func (c *Client) FindByInstance(ctx context.Context, instanceID string) (Container, bool, error) {
	f := c.managedFilter()
	f.Add("label", LabelInstanceID+"="+instanceID)
	list, err := c.cli.ContainerList(ctx, container.ListOptions{All: true, Filters: f})
	if err != nil {
		return Container{}, false, fmt.Errorf("list containers: %w", err)
	}
	if len(list) == 0 {
		return Container{}, false, nil
	}
	return fromSummary(list[0], c.containerImages(ctx, list)[list[0].ID]), true, nil
}

// ListManaged 列出全部受管容器。
func (c *Client) ListManaged(ctx context.Context) ([]Container, error) {
	list, err := c.cli.ContainerList(ctx, container.ListOptions{All: true, Filters: c.managedFilter()})
	if err != nil {
		return nil, fmt.Errorf("list managed containers: %w", err)
	}
	images := c.containerImages(ctx, list)
	out := make([]Container, 0, len(list))
	for _, s := range list {
		out = append(out, fromSummary(s, images[s.ID]))
	}
	return out, nil
}

// Info 返回节点资源与受管容器数。
func (c *Client) Info(ctx context.Context) (NodeInfo, error) {
	info, err := c.cli.Info(ctx)
	if err != nil {
		return NodeInfo{}, fmt.Errorf("docker info: %w", err)
	}
	running, err := c.cli.ContainerList(ctx, container.ListOptions{Filters: c.managedFilter()})
	if err != nil {
		return NodeInfo{}, fmt.Errorf("count managed containers: %w", err)
	}
	return NodeInfo{
		ID:            info.ID,
		Name:          info.Name,
		CPUs:          info.NCPU,
		MemoryBytes:   info.MemTotal,
		Containers:    len(running),
		DockerVersion: info.ServerVersion,
		DockerAPIVer:  c.cli.ClientVersion(),
	}, nil
}

// fromSummary 把 Docker 容器摘要映射为受管容器视图。
//
// image 为该容器自身的 Config.Image（创建时写入的镜像引用，永不漂移），而非摘要里的
// s.Image —— 后者是按镜像 ID 反查当前 tag 的结果，镜像失去全部 tag（悬空）后会退化
// 成哈希。调用方已按需解析并传入；解析失败时回退为摘要值，保证列表仍可用。
func fromSummary(s types.Container, image string) Container {
	name := ""
	if len(s.Names) > 0 {
		name = strings.TrimPrefix(s.Names[0], "/")
	}
	if image == "" {
		image = s.Image
	}
	hostPort, containerPort := portPairFromPorts(s.Ports)
	c := Container{
		ID:            s.ID,
		Name:          name,
		Image:         image,
		State:         s.State,
		Status:        s.Status,
		Labels:        s.Labels,
		InstanceID:    s.Labels[LabelInstanceID],
		HostPort:      hostPort,
		ContainerPort: containerPort,
		IP:            containerIP(s.NetworkSettings),
	}
	// 退出码藏在 Status 文本里（如 "Exited (137) 3 minutes ago"）：列表接口无专门字段，
	// 从文本解析是拿到退出码 / OOM 线索的低成本方式，避免逐容器 Inspect。
	if s.State != "running" {
		c.ExitCode, c.OOMKilled = parseExit(s.Status)
	}
	return c
}

// containerImages 解析每个容器的镜像引用（Config.Image），按容器与镜像 ID 缓存：
// 仅在首次见到某容器（或它换了镜像）时做一次 Inspect。任一容器解析失败都退回
// 摘要里的镜像字段，单个失败不影响整表。
func (c *Client) containerImages(ctx context.Context, list []types.Container) map[string]string {
	out := make(map[string]string, len(list))
	if len(list) == 0 {
		return out
	}

	c.imageMu.Lock()
	defer c.imageMu.Unlock()
	for _, s := range list {
		key := imageRefKey(s.ID, s.ImageID)
		ref, ok := c.imageRefs[key]
		if !ok {
			insp, err := c.cli.ContainerInspect(ctx, s.ID)
			if err != nil || insp.Config == nil || insp.Config.Image == "" {
				continue // 不回填缓存：下次仍会重试，避免把失败结果固化
			}
			ref = insp.Config.Image
			c.imageRefs[key] = ref
		}
		out[s.ID] = ref
	}
	return out
}

// parseExit 从 "Exited (137) ..." / "Exited (0) ..." 解析退出码；137 常见于 OOM/SIGKILL。
func parseExit(status string) (int, bool) {
	open := strings.IndexByte(status, '(')
	closeIdx := strings.IndexByte(status, ')')
	if open < 0 || closeIdx < 0 || closeIdx <= open {
		return 0, false
	}
	code, err := strconv.Atoi(strings.TrimSpace(status[open+1 : closeIdx]))
	if err != nil {
		return 0, false
	}
	return code, code == 137
}

// portPairFromPorts 从端口映射取首个本机映射端口及其对应的容器内部端口。
// 二者取自同一条映射（PrivatePort ↔ PublicPort），保证前端展示成对不串。
func portPairFromPorts(ports []types.Port) (host, container int) {
	for _, p := range ports {
		if p.PublicPort > 0 {
			return int(p.PublicPort), int(p.PrivatePort)
		}
	}
	return 0, 0
}

// containerIP 从摘要的网络设置取容器 IP。Networks 是 map，遍历顺序随机，
// 故按网络名排序后取首个非空地址，保证同一容器每次展示的 IP 稳定。
func containerIP(ns *types.SummaryNetworkSettings) string {
	if ns == nil || len(ns.Networks) == 0 {
		return ""
	}
	names := make([]string, 0, len(ns.Networks))
	for name := range ns.Networks {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if ep := ns.Networks[name]; ep != nil && ep.IPAddress != "" {
			return ep.IPAddress
		}
	}
	return ""
}

// hostPortFromInspect 从 Inspect 结果取指定容器端口对应的本机端口。
func hostPortFromInspect(insp types.ContainerJSON, containerPort int) int {
	if insp.NetworkSettings == nil {
		return 0
	}
	key := nat.Port(fmt.Sprintf("%d/tcp", containerPort))
	if bindings, ok := insp.NetworkSettings.Ports[key]; ok {
		for _, b := range bindings {
			if b.HostPort != "" {
				return atoiSafe(b.HostPort)
			}
		}
	}
	return 0
}

// shortID 取实例 ID 前 12 位用于容器名（UUID 去连字符后）。
func shortID(id string) string {
	s := strings.ReplaceAll(id, "-", "")
	if len(s) > 12 {
		return s[:12]
	}
	return s
}

// parseMemory 把 "256m"/"1g"/"512m" 解析为字节；解析失败返回 0。
func parseMemory(s string) int64 {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}
	mult := int64(1)
	switch s[len(s)-1] {
	case 'k', 'K':
		mult = 1024
		s = s[:len(s)-1]
	case 'm', 'M':
		mult = 1024 * 1024
		s = s[:len(s)-1]
	case 'g', 'G':
		mult = 1024 * 1024 * 1024
		s = s[:len(s)-1]
	}
	n := int64(0)
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0
		}
		n = n*10 + int64(r-'0')
	}
	return n * mult
}

func atoiSafe(s string) int {
	n := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0
		}
		n = n*10 + int(r-'0')
	}
	return n
}
