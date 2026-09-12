// Package gwclient 是 Container Manager 的两类出站 HTTP 客户端：
//   - AgentClient：CM → 节点 Agent，下发容器操作、采集容器实际态。
//   - GatewayClient：CM → Gateway，上报节点/实例状态（状态上行）。
package gwclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// AgentClient 是 CM 指向某节点 Node Agent 的客户端。
type AgentClient struct {
	baseURL string
	token   string
	http    *http.Client
}

// NewAgentClient 构造。
func NewAgentClient(baseURL, token string) *AgentClient {
	return &AgentClient{
		baseURL: baseURL,
		token:   token,
		http:    &http.Client{Timeout: 15 * time.Second},
	}
}

// Container 是 Agent 上报的受管容器视图（与 nodeagent/docker.Container 对应）。
type Container struct {
	ID         string            `json:"id"`
	Name       string            `json:"name"`
	Image      string            `json:"image"`
	State      string            `json:"state"`
	Status     string            `json:"status"`
	Labels     map[string]string `json:"labels"`
	InstanceID string            `json:"instance_id"`
	HostPort   int               `json:"host_port"`
	// 退出信息：非 running 容器的诊断线索。
	ExitCode   int    `json:"exit_code"`
	OOMKilled  bool   `json:"oom_killed"`
	FinishedAt string `json:"finished_at,omitempty"`
}

// NodeInfo 是 Agent 上报的节点资源摘要。
type NodeInfo struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	CPUs          int    `json:"cpus"`
	MemoryBytes   int64  `json:"memory_bytes"`
	Containers    int    `json:"containers"`
	DockerVersion string `json:"docker_version"`
}

// HostMetrics 是节点主机资源使用率（与 nodeagent/hostmetrics.Metrics 对应）。
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

// CreateSpec 是下发给 Agent 的容器创建规格。
type CreateSpec struct {
	InstanceID   string            `json:"instance_id"`
	ServiceID    string            `json:"service_id"`
	DeploymentID string            `json:"deployment_id"`
	VersionID    string            `json:"version_id"`
	Image        string            `json:"image"`
	Port         int               `json:"port"`
	HostPort     int               `json:"host_port"`
	Env          map[string]string `json:"env,omitempty"`
	Memory       string            `json:"memory,omitempty"`
	Command      []string          `json:"command,omitempty"`
	HealthPath   string            `json:"health_path,omitempty"`
}

// CreateResult 是 Agent 创建容器的返回。
type CreateResult struct {
	ID         string `json:"id"`
	InstanceID string `json:"instance_id"`
	HostPort   int    `json:"host_port"`
}

// Health 探测 Agent 存活。
func (c *AgentClient) Health(ctx context.Context) error {
	return c.do(ctx, http.MethodGet, "/health", nil, nil)
}

// Info 拉取节点资源。
func (c *AgentClient) Info(ctx context.Context) (NodeInfo, error) {
	var out NodeInfo
	if err := c.do(ctx, http.MethodGet, "/api/internal/node/info", nil, &out); err != nil {
		return NodeInfo{}, err
	}
	return out, nil
}

// Containers 拉取受管容器列表。
func (c *AgentClient) Containers(ctx context.Context) ([]Container, error) {
	var out []Container
	if err := c.do(ctx, http.MethodGet, "/api/internal/containers", nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// Create 在节点上幂等创建并启动容器。
func (c *AgentClient) Create(ctx context.Context, spec CreateSpec) (CreateResult, error) {
	var out CreateResult
	if err := c.do(ctx, http.MethodPost, "/api/internal/containers", spec, &out); err != nil {
		return CreateResult{}, err
	}
	return out, nil
}

// Stop 停止容器（id 可为容器 ID 或 instance_id）。
func (c *AgentClient) Stop(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodPost, fmt.Sprintf("/api/internal/containers/%s/stop", id), nil, nil)
}

// Restart 重启容器（id 可为容器 ID 或 instance_id）。
func (c *AgentClient) Restart(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodPost, fmt.Sprintf("/api/internal/containers/%s/restart", id), nil, nil)
}

// Start 启动容器（id 可为容器 ID 或 instance_id）。
func (c *AgentClient) Start(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodPost, fmt.Sprintf("/api/internal/containers/%s/start", id), nil, nil)
}

// Logs 读取容器最近 tail 行日志。
func (c *AgentClient) Logs(ctx context.Context, id string, tail int) (string, error) {
	var out struct {
		Logs string `json:"logs"`
	}
	if err := c.do(ctx, http.MethodGet,
		fmt.Sprintf("/api/internal/containers/%s/logs?tail=%d", id, tail), nil, &out); err != nil {
		return "", err
	}
	return out.Logs, nil
}

// Metrics 拉取节点主机与 Docker 指标。
func (c *AgentClient) Metrics(ctx context.Context) (NodeMetrics, error) {
	var out NodeMetrics
	if err := c.do(ctx, http.MethodGet, "/api/internal/node/metrics", nil, &out); err != nil {
		return NodeMetrics{}, err
	}
	return out, nil
}

// Remove 删除容器（id 可为容器 ID 或 instance_id）。
func (c *AgentClient) Remove(ctx context.Context, id string, force bool) error {
	path := fmt.Sprintf("/api/internal/containers/%s", id)
	if force {
		path += "?force=true"
	}
	return c.do(ctx, http.MethodDelete, path, nil, nil)
}

// do 发送请求；in 非空则 JSON 编码，out 非空则解码响应体。
func (c *AgentClient) do(ctx context.Context, method, path string, in, out any) error {
	var body io.Reader
	if in != nil {
		raw, err := json.Marshal(in)
		if err != nil {
			return fmt.Errorf("marshal agent request: %w", err)
		}
		body = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
	if err != nil {
		return fmt.Errorf("build agent request: %w", err)
	}
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("call agent: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return fmt.Errorf("read agent response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("agent returned %d: %s", resp.StatusCode, string(raw))
	}
	if out != nil && len(raw) > 0 {
		if err := json.Unmarshal(raw, out); err != nil {
			return fmt.Errorf("decode agent response: %w", err)
		}
	}
	return nil
}
