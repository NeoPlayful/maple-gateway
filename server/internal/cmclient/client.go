// Package cmclient 是 Gateway 面向 Container Manager 的轻量客户端。
//
// 控制下行：部署意图由 Gateway（Leader）主动推给 CM（贴合 Gateway → CM 链路语义），
// 而非 CM 反向拉取。多 Gateway 实例时仅 Leader 推送，避免重复下发。
// CM 未配置（BaseURL 为空）时客户端为 no-op：Gateway 行为与未接入 CM 时完全一致。
package cmclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/google/uuid"
)

// DesiredState 是下发给 CM 的部署期望态（一个 deployment_versions 版本对应一份）。
type DesiredState struct {
	DeploymentID uuid.UUID `json:"deployment_id"`
	ServiceID    uuid.UUID `json:"service_id"`
	VersionID    uuid.UUID `json:"version_id"`
	Version      string    `json:"version"`
	// Status 版本角色（stable/active/canary/standby/draining/inactive）：
	// CM 据此区分"目标版本"与"待退役版本"，按发布形态决定增减序。
	Status       string            `json:"status,omitempty"`
	Image        string            `json:"image"`
	Replicas     int               `json:"replicas"`
	Port         int               `json:"port"`
	Env          map[string]string `json:"env,omitempty"`
	Resources    json.RawMessage   `json:"resources,omitempty"`
	HealthPath   string            `json:"health_path,omitempty"`
	NodeSelector map[string]string `json:"node_selector,omitempty"`
	Strategy     string            `json:"strategy,omitempty"`
}

// Client 是 Gateway → CM 的 HTTP 客户端。
type Client struct {
	baseURL string
	token   string
	http    *http.Client
}

// New 构造。baseURL 为空表示未接入 CM，返回的客户端所有方法为 no-op。
func New(baseURL, token string) *Client {
	return &Client{
		baseURL: baseURL,
		token:   token,
		http:    &http.Client{Timeout: 10 * time.Second},
	}
}

// Enabled 报告是否已配置 CM 地址。
func (c *Client) Enabled() bool { return c.baseURL != "" }

// PushDeploy 下发/更新部署期望态：POST {cm}/api/internal/deployments。
func (c *Client) PushDeploy(ctx context.Context, in DesiredState) error {
	if !c.Enabled() {
		return nil
	}
	return c.do(ctx, http.MethodPost, "/api/internal/deployments", in, nil)
}

// StopDeploy 停止部署：POST {cm}/api/internal/deployments/{id}/stop。
func (c *Client) StopDeploy(ctx context.Context, deploymentID uuid.UUID) error {
	if !c.Enabled() {
		return nil
	}
	return c.do(ctx, http.MethodPost,
		fmt.Sprintf("/api/internal/deployments/%s/stop", deploymentID), nil, nil)
}

// Status 查询编排进度：GET {cm}/api/internal/deployments/{id}/status，返回原始 JSON。
func (c *Client) Status(ctx context.Context, deploymentID uuid.UUID) (json.RawMessage, error) {
	if !c.Enabled() {
		return json.RawMessage(`{}`), nil
	}
	var out json.RawMessage
	if err := c.do(ctx, http.MethodGet,
		fmt.Sprintf("/api/internal/deployments/%s/status", deploymentID), nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// Overview 拉取 CM 管理总览（观测统计 + 部署进度 + 节点状态）：GET {cm}/api/internal/mgmt/overview。
func (c *Client) Overview(ctx context.Context) (json.RawMessage, error) {
	return c.mgmtGet(ctx, "/api/internal/mgmt/overview")
}

// MgmtNodes 拉取节点与 Agent 连接态：GET {cm}/api/internal/mgmt/nodes。
func (c *Client) MgmtNodes(ctx context.Context) (json.RawMessage, error) {
	return c.mgmtGet(ctx, "/api/internal/mgmt/nodes")
}

// MgmtMetrics 拉取各节点资源指标：GET {cm}/api/internal/mgmt/metrics。
func (c *Client) MgmtMetrics(ctx context.Context) (json.RawMessage, error) {
	return c.mgmtGet(ctx, "/api/internal/mgmt/metrics")
}

// MgmtErrors 拉取运行时错误列表：GET {cm}/api/internal/mgmt/errors。
func (c *Client) MgmtErrors(ctx context.Context) (json.RawMessage, error) {
	return c.mgmtGet(ctx, "/api/internal/mgmt/errors")
}

// mgmtGet 发起一次管理读请求，未接入 CM 时返回空对象。
func (c *Client) mgmtGet(ctx context.Context, path string) (json.RawMessage, error) {
	if !c.Enabled() {
		return json.RawMessage(`{}`), nil
	}
	var out json.RawMessage
	if err := c.do(ctx, http.MethodGet, path, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// RestartInstance 人工重启实例容器：POST {cm}/api/internal/mgmt/instances/{id}/restart。
func (c *Client) RestartInstance(ctx context.Context, instanceID string) error {
	return c.instanceAction(ctx, instanceID, "restart")
}

// StopInstance 人工停止实例容器：POST {cm}/api/internal/mgmt/instances/{id}/stop。
func (c *Client) StopInstance(ctx context.Context, instanceID string) error {
	return c.instanceAction(ctx, instanceID, "stop")
}

// StartInstance 人工启动实例容器：POST {cm}/api/internal/mgmt/instances/{id}/start。
func (c *Client) StartInstance(ctx context.Context, instanceID string) error {
	return c.instanceAction(ctx, instanceID, "start")
}

// InstanceLogs 读取实例容器日志：GET {cm}/api/internal/mgmt/instances/{id}/logs?tail=n。
func (c *Client) InstanceLogs(ctx context.Context, instanceID string, tail int) (json.RawMessage, error) {
	if !c.Enabled() {
		return json.RawMessage(`{"logs":""}`), nil
	}
	var out json.RawMessage
	if err := c.do(ctx, http.MethodGet,
		fmt.Sprintf("/api/internal/mgmt/instances/%s/logs?tail=%d", instanceID, tail), nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// instanceAction 发起一次人工实例操作。
func (c *Client) instanceAction(ctx context.Context, instanceID, action string) error {
	if !c.Enabled() {
		return fmt.Errorf("container manager 未接入")
	}
	return c.do(ctx, http.MethodPost,
		fmt.Sprintf("/api/internal/mgmt/instances/%s/%s", instanceID, action), nil, nil)
}

// do 发送一次请求；body 非空则 JSON 编码，out 非空则解码响应体。
func (c *Client) do(ctx context.Context, method, path string, body any, out *json.RawMessage) error {
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("marshal cm request: %w", err)
		}
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reader)
	if err != nil {
		return fmt.Errorf("build cm request: %w", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("call cm: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return fmt.Errorf("read cm response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("cm returned %d: %s", resp.StatusCode, string(raw))
	}
	if out != nil && len(raw) > 0 {
		*out = json.RawMessage(raw)
	}
	return nil
}
