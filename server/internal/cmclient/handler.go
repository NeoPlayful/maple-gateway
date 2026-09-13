// Package cmclient 的 handler 提供管理面（/api/admin/cm/*）的 CM 聚合代理，
// 使前端始终只与 Gateway 单一入口对话：用户会话、RBAC、审计自动继承。
package cmclient

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net/http"
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
	MgmtEvents(ctx context.Context) (json.RawMessage, error)
	RestartInstance(ctx context.Context, instanceID string) error
	StopInstance(ctx context.Context, instanceID string) error
	StartInstance(ctx context.Context, instanceID string) error
	InstanceLogs(ctx context.Context, instanceID string, tail int) (json.RawMessage, error)
	FollowInstanceLogs(ctx context.Context, instanceID string, tail int) (*http.Response, error)
	EnrollmentTokens(ctx context.Context) (json.RawMessage, error)
	IssueEnrollmentToken(ctx context.Context, body json.RawMessage) (json.RawMessage, error)
	RevokeEnrollmentToken(ctx context.Context, id string) error
	MgmtTasks(ctx context.Context) (json.RawMessage, error)
	TaskDetail(ctx context.Context, id string) (json.RawMessage, error)
	RetryTask(ctx context.Context, id string) (json.RawMessage, error)
	CancelTask(ctx context.Context, id string, body json.RawMessage) error
	Applications(ctx context.Context) (json.RawMessage, error)
	ApplicationDetail(ctx context.Context, id string) (json.RawMessage, error)
	SaveApplication(ctx context.Context, body json.RawMessage) (json.RawMessage, error)
	DeployApplication(ctx context.Context, id string) (json.RawMessage, error)
	StopApplication(ctx context.Context, id string) (json.RawMessage, error)
	StartApplication(ctx context.Context, id string) (json.RawMessage, error)
	RestartApplication(ctx context.Context, id string) (json.RawMessage, error)
	RemoveApplication(ctx context.Context, id string) error
	ApplicationPs(ctx context.Context, id string) (json.RawMessage, error)
	ValidateApplication(ctx context.Context, id string) (json.RawMessage, error)
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

// Events GET /api/admin/cm/events
func (h *Handler) Events(c fiber.Ctx) error {
	if err := h.enabled(); err != nil {
		return pkg.Err(c, err)
	}
	out, err := h.cm.MgmtEvents(c.Context())
	if err != nil {
		return pkg.Err(c, pkg.ErrSystem("拉取容器事件失败: "+err.Error()))
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

// EnrollmentTokens GET /api/admin/cm/enrollment-tokens
func (h *Handler) EnrollmentTokens(c fiber.Ctx) error {
	if err := h.enabled(); err != nil {
		return pkg.Err(c, err)
	}
	out, err := h.cm.EnrollmentTokens(c.Context())
	if err != nil {
		return pkg.Err(c, pkg.ErrSystem("拉取注册令牌失败: "+err.Error()))
	}
	return raw(c, out)
}

// IssueEnrollmentToken POST /api/admin/cm/enrollment-tokens
// 请求体透传 CM（note/ttl_seconds），并把当前管理员 ID 记为签发人。
func (h *Handler) IssueEnrollmentToken(c fiber.Ctx) error {
	if err := h.enabled(); err != nil {
		return pkg.Err(c, err)
	}
	body := c.Body()
	if len(body) == 0 {
		body = []byte(`{}`)
	}
	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		return pkg.Err(c, pkg.ErrValidation("请求体格式错误"))
	}
	m["created_by"] = adminID(c)
	full, _ := json.Marshal(m)
	out, err := h.cm.IssueEnrollmentToken(c.Context(), json.RawMessage(full))
	if err != nil {
		return pkg.Err(c, pkg.ErrSystem("签发注册令牌失败: "+err.Error()))
	}
	return raw(c, out)
}

// RevokeEnrollmentToken DELETE /api/admin/cm/enrollment-tokens/:id
func (h *Handler) RevokeEnrollmentToken(c fiber.Ctx) error {
	if err := h.enabled(); err != nil {
		return pkg.Err(c, err)
	}
	if err := h.cm.RevokeEnrollmentToken(c.Context(), c.Params("id")); err != nil {
		return pkg.Err(c, pkg.ErrSystem("撤销注册令牌失败: "+err.Error()))
	}
	return pkg.OK(c, fiber.Map{"revoked": true})
}

// Tasks GET /api/admin/cm/tasks
func (h *Handler) Tasks(c fiber.Ctx) error {
	if err := h.enabled(); err != nil {
		return pkg.Err(c, err)
	}
	out, err := h.cm.MgmtTasks(c.Context())
	if err != nil {
		return pkg.Err(c, pkg.ErrSystem("拉取任务列表失败: "+err.Error()))
	}
	return raw(c, out)
}

// TaskDetail GET /api/admin/cm/tasks/:id
func (h *Handler) TaskDetail(c fiber.Ctx) error {
	if err := h.enabled(); err != nil {
		return pkg.Err(c, err)
	}
	out, err := h.cm.TaskDetail(c.Context(), c.Params("id"))
	if err != nil {
		return pkg.Err(c, pkg.ErrSystem("拉取任务详情失败: "+err.Error()))
	}
	return raw(c, out)
}

// RetryTask POST /api/admin/cm/tasks/:id/retry
func (h *Handler) RetryTask(c fiber.Ctx) error {
	if err := h.enabled(); err != nil {
		return pkg.Err(c, err)
	}
	out, err := h.cm.RetryTask(c.Context(), c.Params("id"))
	if err != nil {
		return pkg.Err(c, pkg.ErrSystem("重试任务失败: "+err.Error()))
	}
	return raw(c, out)
}

// CancelTask POST /api/admin/cm/tasks/:id/cancel
func (h *Handler) CancelTask(c fiber.Ctx) error {
	if err := h.enabled(); err != nil {
		return pkg.Err(c, err)
	}
	body := c.Body()
	if len(body) == 0 {
		body = []byte(`{}`)
	}
	if err := h.cm.CancelTask(c.Context(), c.Params("id"), json.RawMessage(body)); err != nil {
		return pkg.Err(c, pkg.ErrSystem("取消任务失败: "+err.Error()))
	}
	return pkg.OK(c, fiber.Map{"cancelled": true})
}

// Applications GET /api/admin/cm/applications
func (h *Handler) Applications(c fiber.Ctx) error {
	if err := h.enabled(); err != nil {
		return pkg.Err(c, err)
	}
	out, err := h.cm.Applications(c.Context())
	if err != nil {
		return pkg.Err(c, pkg.ErrSystem("拉取应用列表失败: "+err.Error()))
	}
	return raw(c, out)
}

// ApplicationDetail GET /api/admin/cm/applications/:id
func (h *Handler) ApplicationDetail(c fiber.Ctx) error {
	if err := h.enabled(); err != nil {
		return pkg.Err(c, err)
	}
	out, err := h.cm.ApplicationDetail(c.Context(), c.Params("id"))
	if err != nil {
		return pkg.Err(c, pkg.ErrSystem("拉取应用详情失败: "+err.Error()))
	}
	return raw(c, out)
}

// SaveApplication POST /api/admin/cm/applications
func (h *Handler) SaveApplication(c fiber.Ctx) error {
	if err := h.enabled(); err != nil {
		return pkg.Err(c, err)
	}
	body := c.Body()
	if len(body) == 0 {
		body = []byte(`{}`)
	}
	out, err := h.cm.SaveApplication(c.Context(), json.RawMessage(body))
	if err != nil {
		return pkg.Err(c, pkg.ErrSystem("保存应用失败: "+err.Error()))
	}
	return raw(c, out)
}

// DeployApplication POST /api/admin/cm/applications/:id/deploy
func (h *Handler) DeployApplication(c fiber.Ctx) error {
	if err := h.enabled(); err != nil {
		return pkg.Err(c, err)
	}
	out, err := h.cm.DeployApplication(c.Context(), c.Params("id"))
	if err != nil {
		return pkg.Err(c, pkg.ErrSystem("部署应用失败: "+err.Error()))
	}
	return raw(c, out)
}

// StopApplication POST /api/admin/cm/applications/:id/stop
func (h *Handler) StopApplication(c fiber.Ctx) error {
	if err := h.enabled(); err != nil {
		return pkg.Err(c, err)
	}
	out, err := h.cm.StopApplication(c.Context(), c.Params("id"))
	if err != nil {
		return pkg.Err(c, pkg.ErrSystem("停止应用失败: "+err.Error()))
	}
	return raw(c, out)
}

// StartApplication POST /api/admin/cm/applications/:id/start
func (h *Handler) StartApplication(c fiber.Ctx) error {
	if err := h.enabled(); err != nil {
		return pkg.Err(c, err)
	}
	out, err := h.cm.StartApplication(c.Context(), c.Params("id"))
	if err != nil {
		return pkg.Err(c, pkg.ErrSystem("启动应用失败: "+err.Error()))
	}
	return raw(c, out)
}

// RestartApplication POST /api/admin/cm/applications/:id/restart
func (h *Handler) RestartApplication(c fiber.Ctx) error {
	if err := h.enabled(); err != nil {
		return pkg.Err(c, err)
	}
	out, err := h.cm.RestartApplication(c.Context(), c.Params("id"))
	if err != nil {
		return pkg.Err(c, pkg.ErrSystem("重启应用失败: "+err.Error()))
	}
	return raw(c, out)
}

// RemoveApplication DELETE /api/admin/cm/applications/:id
func (h *Handler) RemoveApplication(c fiber.Ctx) error {
	if err := h.enabled(); err != nil {
		return pkg.Err(c, err)
	}
	if err := h.cm.RemoveApplication(c.Context(), c.Params("id")); err != nil {
		return pkg.Err(c, pkg.ErrSystem("移除应用失败: "+err.Error()))
	}
	return pkg.OK(c, fiber.Map{"removed": true})
}

// ApplicationPs GET /api/admin/cm/applications/:id/ps
func (h *Handler) ApplicationPs(c fiber.Ctx) error {
	if err := h.enabled(); err != nil {
		return pkg.Err(c, err)
	}
	out, err := h.cm.ApplicationPs(c.Context(), c.Params("id"))
	if err != nil {
		return pkg.Err(c, pkg.ErrSystem("拉取应用服务状态失败: "+err.Error()))
	}
	return raw(c, out)
}

// ValidateApplication POST /api/admin/cm/applications/:id/validate
func (h *Handler) ValidateApplication(c fiber.Ctx) error {
	if err := h.enabled(); err != nil {
		return pkg.Err(c, err)
	}
	out, err := h.cm.ValidateApplication(c.Context(), c.Params("id"))
	if err != nil {
		return pkg.Err(c, pkg.ErrSystem("校验应用失败: "+err.Error()))
	}
	return raw(c, out)
}

// InstanceLogsStream GET /api/admin/cm/instances/:id/logs/stream
// 把 CM 的实时日志流分块转发给前端（透传纯文本，边读边 flush）。
func (h *Handler) InstanceLogsStream(c fiber.Ctx) error {
	if err := h.enabled(); err != nil {
		return pkg.Err(c, err)
	}
	tail, _ := strconv.Atoi(c.Query("tail", "200"))
	resp, err := h.cm.FollowInstanceLogs(c.Context(), c.Params("id"), tail)
	if err != nil {
		return pkg.Err(c, pkg.ErrSystem("打开日志流失败: "+err.Error()))
	}
	defer func() { _ = resp.Body.Close() }()

	c.Set("Content-Type", "text/plain; charset=utf-8")
	c.Set("Cache-Control", "no-cache")
	c.Set("X-Accel-Buffering", "no")
	return c.SendStreamWriter(func(w *bufio.Writer) {
		buf := make([]byte, 4096)
		for {
			n, err := resp.Body.Read(buf)
			if n > 0 {
				if _, werr := w.Write(buf[:n]); werr != nil {
					return
				}
				if ferr := w.Flush(); ferr != nil {
					return
				}
			}
			if err != nil {
				if err != io.EOF {
					_, _ = w.WriteString("\n[日志流中断]")
					_ = w.Flush()
				}
				return
			}
		}
	})
}

// adminID 从上下文取当前管理员 ID（未识别时返回空串）。
func adminID(c fiber.Ctx) string {
	if v, ok := c.Locals("auth.adminID").(string); ok {
		return v
	}
	return ""
}
