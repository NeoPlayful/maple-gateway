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
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
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
}

// New 构造。managedLabel 为受管标签键；host 为空则用 SDK 默认（/var/run/docker.sock）。
func New(host, managedLabel string) (*Client, error) {
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
	return &Client{cli: cli, managedLabel: managedLabel}, nil
}

// Close 释放底层连接。
func (c *Client) Close() error { return c.cli.Close() }

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
}

// Container 是受管容器的观测视图。
type Container struct {
	ID         string            `json:"id"`
	Name       string            `json:"name"`
	Image      string            `json:"image"`
	State      string            `json:"state"`  // created / running / exited / …
	Status     string            `json:"status"` // 人类可读状态
	Labels     map[string]string `json:"labels"`
	InstanceID string            `json:"instance_id"`
	HostPort   int               `json:"host_port"` // 本机映射端口（从端口映射解析）
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
}

// DiskUsage 是 Docker 引擎空间占用摘要。
type DiskUsage struct {
	LayersSize int64 `json:"layers_size"` // 镜像层总大小（字节）
	Images     int   `json:"images"`
	Containers int   `json:"containers"`
	Volumes    int   `json:"volumes"`
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
	return fromSummary(list[0]), true, nil
}

// ListManaged 列出全部受管容器。
func (c *Client) ListManaged(ctx context.Context) ([]Container, error) {
	list, err := c.cli.ContainerList(ctx, container.ListOptions{All: true, Filters: c.managedFilter()})
	if err != nil {
		return nil, fmt.Errorf("list managed containers: %w", err)
	}
	out := make([]Container, 0, len(list))
	for _, s := range list {
		out = append(out, fromSummary(s))
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
	}, nil
}

// fromSummary 把 Docker 容器摘要映射为受管容器视图。
func fromSummary(s types.Container) Container {
	name := ""
	if len(s.Names) > 0 {
		name = strings.TrimPrefix(s.Names[0], "/")
	}
	c := Container{
		ID:         s.ID,
		Name:       name,
		Image:      s.Image,
		State:      s.State,
		Status:     s.Status,
		Labels:     s.Labels,
		InstanceID: s.Labels[LabelInstanceID],
		HostPort:   hostPortFromPorts(s.Ports),
	}
	// 退出码藏在 Status 文本里（如 "Exited (137) 3 minutes ago"）：列表接口无专门字段，
	// 从文本解析是拿到退出码 / OOM 线索的低成本方式，避免逐容器 Inspect。
	if s.State != "running" {
		c.ExitCode, c.OOMKilled = parseExit(s.Status)
	}
	return c
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

// hostPortFromPorts 从端口映射中取首个本机端口。
func hostPortFromPorts(ports []types.Port) int {
	for _, p := range ports {
		if p.PublicPort > 0 {
			return int(p.PublicPort)
		}
	}
	return 0
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
