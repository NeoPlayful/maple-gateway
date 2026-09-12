package deployment

import (
	"context"
	"strconv"

	"github.com/NeoPlayful/maple-gateway/server/internal/cmclient"
	"github.com/NeoPlayful/maple-gateway/server/pkg"
	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
)

// DeployPusher 是 Gateway 面向 Container Manager 的下发接口（由 cmclient.Client 实现）。
// 为 nil 时不下发（未接入 CM）。
type DeployPusher interface {
	PushDeploy(ctx context.Context, in cmclient.DesiredState) error
	StopDeploy(ctx context.Context, deploymentID uuid.UUID) error
}

// Handler 暴露 Deployment / Version 的 Management API。
type Handler struct {
	repo   *Repository
	pusher DeployPusher // 可空；接入 CM 后由 Leader 下发部署意图
	leader func() bool  // 可空；nil 视为单实例（始终为 leader）
}

// NewHandler 构造。
func NewHandler(repo *Repository) *Handler {
	return &Handler{repo: repo}
}

// WithPusher 注入 CM 下发通道与 Leader 判定。pusher 为 nil 表示未接入 CM。
func (h *Handler) WithPusher(pusher DeployPusher, leader func() bool) *Handler {
	h.pusher = pusher
	h.leader = leader
	return h
}

// isLeader 报告本进程是否应执行下发；未注入判定时视为单实例 leader。
func (h *Handler) isLeader() bool {
	return h.leader == nil || h.leader()
}

// pushVersion 在版本规格可下发时（有 image）由 Leader 推送期望态给 CM。
// 下发失败仅记日志、不回滚版本变更：配置已落库，CM 侧对账会收敛。
func (h *Handler) pushVersion(c fiber.Ctx, v *Version, strategy string) {
	if h.pusher == nil || v == nil || v.Image == "" || !h.isLeader() {
		return
	}
	dep, err := h.repo.GetDeployment(c.Context(), v.DeploymentID)
	if err != nil {
		return
	}
	in := cmclient.DesiredState{
		DeploymentID: v.DeploymentID,
		ServiceID:    dep.ServiceID,
		VersionID:    v.ID,
		Version:      v.Version,
		Status:       string(v.Status),
		Image:        v.Image,
		Replicas:     v.Replicas,
		Port:         v.Port,
		Env:          v.Env,
		Resources:    v.Resources,
		HealthPath:   v.HealthPath,
		NodeSelector: v.NodeSelector,
		Strategy:     strategy,
	}
	if err := h.pusher.PushDeploy(c.Context(), in); err != nil {
		pkg.Log().Warn("push deployment intent to cm failed: " + err.Error())
	}
}

func parseID(c fiber.Ctx, name string) (uuid.UUID, error) {
	id, err := uuid.Parse(c.Params(name))
	if err != nil {
		return uuid.Nil, pkg.ErrValidation("无效的 " + name + " ID")
	}
	return id, nil
}

func parseLimitOffset(c fiber.Ctx) (int, int) {
	limit, _ := strconv.Atoi(c.Query("limit", "20"))
	offset, _ := strconv.Atoi(c.Query("offset", "0"))
	if limit <= 0 || limit > 200 {
		limit = 20
	}
	return limit, offset
}

// ---------- Deployment ----------

// ListDeployments GET /api/admin/deployments?service_id=&limit=&offset=
func (h *Handler) ListDeployments(c fiber.Ctx) error {
	var serviceID *uuid.UUID
	if q := c.Query("service_id"); q != "" {
		id, err := uuid.Parse(q)
		if err != nil {
			return pkg.Err(c, pkg.ErrValidation("无效的 service_id"))
		}
		serviceID = &id
	}
	limit, offset := parseLimitOffset(c)
	items, total, err := h.repo.ListDeployments(c.Context(), serviceID, limit, offset)
	if err != nil {
		return pkg.Err(c, err)
	}
	return pkg.OKMeta(c, items, fiber.Map{"total": total, "limit": limit, "offset": offset})
}

// CreateDeployment POST /api/admin/deployments
func (h *Handler) CreateDeployment(c fiber.Ctx) error {
	var in NewDeployment
	if err := c.Bind().Body(&in); err != nil {
		return pkg.Err(c, pkg.ErrValidation("请求体格式错误"))
	}
	if err := pkg.ValidateStruct(in); err != nil {
		return pkg.Err(c, err)
	}
	d, err := h.repo.CreateDeployment(c.Context(), in)
	if err != nil {
		return pkg.Err(c, err)
	}
	return pkg.OK(c, d)
}

// GetDeployment GET /api/admin/deployments/:id
func (h *Handler) GetDeployment(c fiber.Ctx) error {
	id, err := parseID(c, "id")
	if err != nil {
		return pkg.Err(c, err)
	}
	d, err := h.repo.GetDeployment(c.Context(), id)
	if err != nil {
		return pkg.Err(c, err)
	}
	return pkg.OK(c, d)
}

// UpdateDeployment PATCH /api/admin/deployments/:id
func (h *Handler) UpdateDeployment(c fiber.Ctx) error {
	id, err := parseID(c, "id")
	if err != nil {
		return pkg.Err(c, err)
	}
	var in UpdateDeployment
	if err := c.Bind().Body(&in); err != nil {
		return pkg.Err(c, pkg.ErrValidation("请求体格式错误"))
	}
	d, err := h.repo.UpdateDeployment(c.Context(), id, in)
	if err != nil {
		return pkg.Err(c, err)
	}
	return pkg.OK(c, d)
}

// DeleteDeployment DELETE /api/admin/deployments/:id
func (h *Handler) DeleteDeployment(c fiber.Ctx) error {
	id, err := parseID(c, "id")
	if err != nil {
		return pkg.Err(c, err)
	}
	if err := h.repo.DeleteDeployment(c.Context(), id); err != nil {
		return pkg.Err(c, err)
	}
	return pkg.OK(c, fiber.Map{"deleted": true})
}

// PauseDeployment POST /api/admin/deployments/:id/pause
func (h *Handler) PauseDeployment(c fiber.Ctx) error { return h.setDeploymentStatus(c, StatusPaused) }

// ResumeDeployment POST /api/admin/deployments/:id/resume
func (h *Handler) ResumeDeployment(c fiber.Ctx) error { return h.setDeploymentStatus(c, StatusActive) }

// StopDeployment POST /api/admin/deployments/:id/stop
func (h *Handler) StopDeployment(c fiber.Ctx) error { return h.setDeploymentStatus(c, StatusStopped) }

func (h *Handler) setDeploymentStatus(c fiber.Ctx, s Status) error {
	id, err := parseID(c, "id")
	if err != nil {
		return pkg.Err(c, err)
	}
	d, err := h.repo.SetDeploymentStatus(c.Context(), id, s)
	if err != nil {
		return pkg.Err(c, err)
	}
	// 停止部署：通知 CM 摘除该部署下的容器（仅 Leader 下发）。
	if s == StatusStopped && h.pusher != nil && h.isLeader() {
		if err := h.pusher.StopDeploy(c.Context(), id); err != nil {
			pkg.Log().Warn("stop deployment intent to cm failed: " + err.Error())
		}
	}
	return pkg.OK(c, d)
}

// ---------- Version ----------

// ListVersions GET /api/admin/deployments/:id/versions
func (h *Handler) ListVersions(c fiber.Ctx) error {
	id, err := parseID(c, "id")
	if err != nil {
		return pkg.Err(c, err)
	}
	items, err := h.repo.ListVersions(c.Context(), id)
	if err != nil {
		return pkg.Err(c, err)
	}
	return pkg.OKMeta(c, items, fiber.Map{"total": len(items)})
}

// CreateVersion POST /api/admin/deployments/:id/versions
func (h *Handler) CreateVersion(c fiber.Ctx) error {
	id, err := parseID(c, "id")
	if err != nil {
		return pkg.Err(c, err)
	}
	var in NewVersion
	if err := c.Bind().Body(&in); err != nil {
		return pkg.Err(c, pkg.ErrValidation("请求体格式错误"))
	}
	if err := pkg.ValidateStruct(in); err != nil {
		return pkg.Err(c, err)
	}
	v, err := h.repo.CreateVersion(c.Context(), id, in)
	if err != nil {
		return pkg.Err(c, err)
	}
	h.pushVersion(c, v, "")
	return pkg.OK(c, v)
}

// GetVersion GET /api/admin/versions/:id
func (h *Handler) GetVersion(c fiber.Ctx) error {
	id, err := parseID(c, "id")
	if err != nil {
		return pkg.Err(c, err)
	}
	v, err := h.repo.GetVersion(c.Context(), id)
	if err != nil {
		return pkg.Err(c, err)
	}
	return pkg.OK(c, v)
}

// UpdateVersion PATCH /api/admin/versions/:id
func (h *Handler) UpdateVersion(c fiber.Ctx) error {
	id, err := parseID(c, "id")
	if err != nil {
		return pkg.Err(c, err)
	}
	var in UpdateVersion
	if err := c.Bind().Body(&in); err != nil {
		return pkg.Err(c, pkg.ErrValidation("请求体格式错误"))
	}
	v, err := h.repo.UpdateVersion(c.Context(), id, in)
	if err != nil {
		return pkg.Err(c, err)
	}
	h.pushVersion(c, v, "")
	return pkg.OK(c, v)
}

// DeleteVersion DELETE /api/admin/versions/:id
func (h *Handler) DeleteVersion(c fiber.Ctx) error {
	id, err := parseID(c, "id")
	if err != nil {
		return pkg.Err(c, err)
	}
	if err := h.repo.DeleteVersion(c.Context(), id); err != nil {
		return pkg.Err(c, err)
	}
	return pkg.OK(c, fiber.Map{"deleted": true})
}

// SetDefaultVersion POST /api/admin/versions/:id/default
func (h *Handler) SetDefaultVersion(c fiber.Ctx) error {
	id, err := parseID(c, "id")
	if err != nil {
		return pkg.Err(c, err)
	}
	// 需要所属 deployment_id 限定范围。
	var in struct {
		DeploymentID uuid.UUID `json:"deployment_id"`
	}
	if err := c.Bind().Body(&in); err != nil {
		return pkg.Err(c, pkg.ErrValidation("请求体格式错误"))
	}
	if in.DeploymentID == uuid.Nil {
		return pkg.Err(c, pkg.ErrValidation("缺少 deployment_id"))
	}
	v, err := h.repo.SetDefaultVersion(c.Context(), in.DeploymentID, id)
	if err != nil {
		return pkg.Err(c, err)
	}
	return pkg.OK(c, v)
}
