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

// GatewayClient 是 CM 上报 Gateway 内部 API 的客户端（状态上行）。
// 令牌为 Gateway 的 MAPLE_INTERNAL_TOKEN。
type GatewayClient struct {
	baseURL string
	token   string
	http    *http.Client
}

// NewGatewayClient 构造。
func NewGatewayClient(baseURL, token string) *GatewayClient {
	return &GatewayClient{
		baseURL: baseURL,
		token:   token,
		http:    &http.Client{Timeout: 15 * time.Second},
	}
}

// Enabled 报告是否配置了 Gateway 地址。
func (c *GatewayClient) Enabled() bool { return c.baseURL != "" }

// RegisterNode 上报节点注册，返回 Gateway 侧节点 ID。
func (c *GatewayClient) RegisterNode(ctx context.Context, name, host, region string, labels map[string]string) (string, error) {
	in := map[string]any{"name": name, "host": host, "region": region, "labels": labels}
	var out struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := c.do(ctx, http.MethodPost, "/api/internal/nodes/register", in, &out); err != nil {
		return "", err
	}
	return out.Data.ID, nil
}

// HeartbeatNode 上报节点心跳。
func (c *GatewayClient) HeartbeatNode(ctx context.Context, nodeID string) error {
	return c.do(ctx, http.MethodPost, fmt.Sprintf("/api/internal/nodes/%s/heartbeat", nodeID), nil, nil)
}

// RegisterInstance 上报实例注册（新容器 → 实例）。
func (c *GatewayClient) RegisterInstance(ctx context.Context, in InstanceReport) (string, error) {
	var out struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := c.do(ctx, http.MethodPost, "/api/internal/instances/register", in, &out); err != nil {
		return "", err
	}
	return out.Data.ID, nil
}

// HeartbeatInstance 上报实例心跳（刷新 last_seen）。
func (c *GatewayClient) HeartbeatInstance(ctx context.Context, instanceID string) error {
	return c.do(ctx, http.MethodPost, fmt.Sprintf("/api/internal/instances/%s/heartbeat", instanceID), nil, nil)
}

// DeleteInstance 注销实例（容器消失）。
func (c *GatewayClient) DeleteInstance(ctx context.Context, instanceID string) error {
	return c.do(ctx, http.MethodDelete, fmt.Sprintf("/api/internal/instances/%s", instanceID), nil, nil)
}

// ReportHealth 上报实例健康。
func (c *GatewayClient) ReportHealth(ctx context.Context, instanceID, health string) error {
	in := map[string]string{"health": health}
	return c.do(ctx, http.MethodPost, fmt.Sprintf("/api/internal/instances/%s/health", instanceID), in, nil)
}

// DrainInstance 通知实例进入 draining。
func (c *GatewayClient) DrainInstance(ctx context.Context, instanceID string) error {
	return c.do(ctx, http.MethodPost, fmt.Sprintf("/api/internal/instances/%s/drain", instanceID), nil, nil)
}

// InstanceReport 是上报 Gateway 的实例结构。
// ID 为容器 maple.instance_id 标签（= Gateway 实例 ID），保证一一对应。
type InstanceReport struct {
	ID           string `json:"id"`
	ServiceID    string `json:"service_id"`
	DeploymentID string `json:"deployment_id,omitempty"`
	VersionID    string `json:"version_id,omitempty"`
	NodeID       string `json:"node_id,omitempty"`
	Version      string `json:"version,omitempty"`
	Address      string `json:"address"`
	Port         int    `json:"port"`
	Protocol     string `json:"protocol,omitempty"`
}

// do 发送请求；in 非空则 JSON 编码，out 非空则解码响应体。
func (c *GatewayClient) do(ctx context.Context, method, path string, in, out any) error {
	if !c.Enabled() {
		return fmt.Errorf("gateway base url not configured")
	}
	var body io.Reader
	if in != nil {
		raw, err := json.Marshal(in)
		if err != nil {
			return fmt.Errorf("marshal gateway request: %w", err)
		}
		body = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
	if err != nil {
		return fmt.Errorf("build gateway request: %w", err)
	}
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("call gateway: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return fmt.Errorf("read gateway response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("gateway returned %d: %s", resp.StatusCode, string(raw))
	}
	if out != nil && len(raw) > 0 {
		if err := json.Unmarshal(raw, out); err != nil {
			return fmt.Errorf("decode gateway response: %w", err)
		}
	}
	return nil
}
