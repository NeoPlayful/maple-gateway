// Package cmclient 的 handler 提供管理面（/api/admin/cm/*）的 CM 聚合代理，
// 使前端始终只与 Gateway 单一入口对话：用户会话、RBAC、审计自动继承。
package cmclient

import (
	"context"
	"encoding/json"
	"strconv"

	"github.com/NeoPlayful/maple-gateway/server/pkg"
	"github.com/gofiber/fiber/v3"
)

// Proxier 是管理代理所需的 CM 能力（由 *Client 实现；便于测试替身）。
type Proxier interface {
	Enabled() bool
	Overview(ctx context.Context) (json.RawMessage, error)
	MgmtNodes(ctx context.Context) (json.RawMessage, error)
	MgmtMetrics(ctx context.Context) (json.RawMessage, error)
	MgmtErrors(ctx context.Context) (json.RawMessage, error)
	MgmtContainers(ctx context.Context) (json.RawMessage, error)
	RestartInstance(ctx context.Context, instanceID string) error
	StopInstance(ctx context.Context, instanceID string) error
	StartInstance(ctx context.Context, instanceID string) error
	InstanceLogs(ctx context.Context, instanceID string, tail int) (json.RawMessage, error)
}

// Handler 是 CM 管理代理的 HTTP handler。
type Handler struct {
	cm Proxier
}

// NewHandler 构造。
func NewHandler(cm Proxier) *Handler { return &Handler{cm: cm} }

// enabled 校验 CM 已接入；未接入时返回 503，供前端显示"未启用运行时编排"。
func (h *Handler) enabled() error {
	if h.cm == nil || !h.cm.Enabled() {
		return pkg.NewAppError(503, pkg.CodeSystemError, "Container Manager 未接入")
	}
	return nil
}

// raw 把 CM 返回的原始 JSON 作为 data 透传（保持 {code,message,data} 解包约定）。
func raw(c fiber.Ctx, r json.RawMessage) error {
	return pkg.OK(c, json.RawMessage(r))
}

// Overview GET /api/admin/cm/overview
func (h *Handler) Overview(c fiber.Ctx) error {
	if err := h.enabled(); err != nil {
		return pkg.Err(c, err)
	}
	out, err := h.cm.Overview(c.Context())
	if err != nil {
		return pkg.Err(c, pkg.ErrSystem("拉取 CM 总览失败: "+err.Error()))
	}
	return raw(c, out)
}

// Nodes GET /api/admin/cm/nodes
func (h *Handler) Nodes(c fiber.Ctx) error {
	if err := h.enabled(); err != nil {
		return pkg.Err(c, err)
	}
	out, err := h.cm.MgmtNodes(c.Context())
	if err != nil {
		return pkg.Err(c, pkg.ErrSystem("拉取节点状态失败: "+err.Error()))
	}
	return raw(c, out)
}

// Metrics GET /api/admin/cm/metrics
func (h *Handler) Metrics(c fiber.Ctx) error {
	if err := h.enabled(); err != nil {
		return pkg.Err(c, err)
	}
	out, err := h.cm.MgmtMetrics(c.Context())
	if err != nil {
		return pkg.Err(c, pkg.ErrSystem("拉取节点指标失败: "+err.Error()))
	}
	return raw(c, out)
}

// Containers GET /api/admin/cm/containers
func (h *Handler) Containers(c fiber.Ctx) error {
	if err := h.enabled(); err != nil {
		return pkg.Err(c, err)
	}
	out, err := h.cm.MgmtContainers(c.Context())
	if err != nil {
		return pkg.Err(c, pkg.ErrSystem("拉取受管容器清单失败: "+err.Error()))
	}
	return raw(c, out)
}

// Errors GET /api/admin/cm/errors
func (h *Handler) Errors(c fiber.Ctx) error {
	if err := h.enabled(); err != nil {
		return pkg.Err(c, err)
	}
	out, err := h.cm.MgmtErrors(c.Context())
	if err != nil {
		return pkg.Err(c, pkg.ErrSystem("拉取运行时错误失败: "+err.Error()))
	}
	return raw(c, out)
}

// RestartInstance POST /api/admin/cm/instances/:id/restart
func (h *Handler) RestartInstance(c fiber.Ctx) error {
	if err := h.enabled(); err != nil {
		return pkg.Err(c, err)
	}
	if err := h.cm.RestartInstance(c.Context(), c.Params("id")); err != nil {
		return pkg.Err(c, pkg.ErrSystem("重启实例失败: "+err.Error()))
	}
	return pkg.OK(c, fiber.Map{"restarted": true})
}

// StopInstance POST /api/admin/cm/instances/:id/stop
func (h *Handler) StopInstance(c fiber.Ctx) error {
	if err := h.enabled(); err != nil {
		return pkg.Err(c, err)
	}
	if err := h.cm.StopInstance(c.Context(), c.Params("id")); err != nil {
		return pkg.Err(c, pkg.ErrSystem("停止实例失败: "+err.Error()))
	}
	return pkg.OK(c, fiber.Map{"stopped": true})
}

// StartInstance POST /api/admin/cm/instances/:id/start
func (h *Handler) StartInstance(c fiber.Ctx) error {
	if err := h.enabled(); err != nil {
		return pkg.Err(c, err)
	}
	if err := h.cm.StartInstance(c.Context(), c.Params("id")); err != nil {
		return pkg.Err(c, pkg.ErrSystem("启动实例失败: "+err.Error()))
	}
	return pkg.OK(c, fiber.Map{"started": true})
}

// InstanceLogs GET /api/admin/cm/instances/:id/logs?tail=200
func (h *Handler) InstanceLogs(c fiber.Ctx) error {
	if err := h.enabled(); err != nil {
		return pkg.Err(c, err)
	}
	tail, _ := strconv.Atoi(c.Query("tail", "200"))
	out, err := h.cm.InstanceLogs(c.Context(), c.Params("id"), tail)
	if err != nil {
		return pkg.Err(c, pkg.ErrSystem("读取实例日志失败: "+err.Error()))
	}
	return raw(c, out)
}
