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
	// ForgetDeploy 清除 CM 侧某部署的期望态（不回收容器）。
	ForgetDeploy(ctx context.Context, deploymentID uuid.UUID) error
	// RemoveVersion 回收某部署下指定版本的容器（删除版本时仅回收该版本，不动其它版本）。
	RemoveVersion(ctx context.Context, deploymentID, versionID uuid.UUID) error
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
		Mounts:       cmMounts(v.Mounts),
	}
	if err := h.pusher.PushDeploy(c.Context(), in); err != nil {
		pkg.Log().Warn("push deployment intent to cm failed: " + err.Error())
	}
}

// cmMounts 把领域挂载项转为下发 CM 的形态。
func cmMounts(ms []Mount) []cmclient.Mount {
	if len(ms) == 0 {
		return nil
	}
	out := make([]cmclient.Mount, 0, len(ms))
	for _, m := range ms {
		out = append(out, cmclient.Mount{Path: m.Path, Target: m.Target, ReadOnly: m.ReadOnly})
	}
	return out
}

// syncAction 是 syncDeployment 的决策结果。
type syncAction int

const (
	syncNone   syncAction = iota // 无需动作
	syncStop                     // 无版本：停止部署并回收容器
	syncPush                     // 有可下发版本：重指期望态到该版本
	syncForget                   // 有版本但全无镜像：清除孤儿期望态，保留容器
)

// decideSync 依据剩余版本集合决定如何对齐 CM 期望态：
//
//	无版本            → stop（停止部署、回收容器）
//	有带镜像的首要版本 → push（重指期望态到该版本）
//	有版本但全无镜像   → forget（清掉可能指向已删版本的孤儿期望态，不动幸存容器）
//
// 关键：绝不因「挑不出带镜像版本」就发 stop——stop 会按 deployment_id 回收该部署
// 名下全部容器，把仍在的其他版本一并删掉。forget 只摘期望态、不删容器。
func decideSync(versions []*Version) (syncAction, *Version) {
	if len(versions) == 0 {
		return syncStop, nil
	}
	if target := primaryVersion(versions); target != nil && target.Image != "" {
		return syncPush, target
	}
	return syncForget, nil
}

// syncDeployment 在版本集合变化后按 decideSync 的决策对齐 CM 期望态。
func (h *Handler) syncDeployment(c fiber.Ctx, deploymentID uuid.UUID) {
	if h.pusher == nil || !h.isLeader() {
		return
	}
	versions, err := h.repo.ListVersions(c.Context(), deploymentID)
	if err != nil {
		return
	}
	switch action, target := decideSync(versions); action {
	case syncStop:
		if err := h.pusher.StopDeploy(c.Context(), deploymentID); err != nil {
			pkg.Log().Warn("stop deployment intent to cm failed: " + err.Error())
		}
	case syncPush:
		h.pushVersion(c, target, "")
	case syncForget:
		// 清掉 CM 侧仍指向被删版本的期望态，避免对账器继续补建；幸存版本容器不受影响。
		if err := h.pusher.ForgetDeploy(c.Context(), deploymentID); err != nil {
			pkg.Log().Warn("forget deployment intent to cm failed: " + err.Error())
		}
	}
}

// reclaimVersion 回收被删版本的容器（仅该版本，不波及同部署其他版本）。
func (h *Handler) reclaimVersion(c fiber.Ctx, deploymentID, versionID uuid.UUID) {
	if h.pusher == nil || !h.isLeader() {
		return
	}
	if err := h.pusher.RemoveVersion(c.Context(), deploymentID, versionID); err != nil {
		pkg.Log().Warn("remove version containers from cm failed: " + err.Error())
	}
}

// primaryVersion 从版本集合中挑出应下发的首要版本：优先 stable，其次任意带镜像的版本。
func primaryVersion(versions []*Version) *Version {
	var fallback *Version
	for _, v := range versions {
		if v == nil || v.Image == "" {
			continue
		}
		if v.Status == VersionStable {
			return v
		}
		if fallback == nil {
			fallback = v
		}
	}
	return fallback
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
// 删除后通知 CM 停止该部署并回收容器，避免 CM 侧残留孤儿期望态继续补建。
func (h *Handler) DeleteDeployment(c fiber.Ctx) error {
	id, err := parseID(c, "id")
	if err != nil {
		return pkg.Err(c, err)
	}
	if err := h.repo.DeleteDeployment(c.Context(), id); err != nil {
		return pkg.Err(c, err)
	}
	if h.pusher != nil && h.isLeader() {
		if err := h.pusher.StopDeploy(c.Context(), id); err != nil {
			pkg.Log().Warn("stop deployment intent to cm failed: " + err.Error())
		}
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

// ListAllVersions GET /api/admin/versions —— 平铺全部版本（带服务/部署归属），供全局版本列表。
func (h *Handler) ListAllVersions(c fiber.Ctx) error {
	items, err := h.repo.ListAllVersions(c.Context())
	if err != nil {
		return pkg.Err(c, err)
	}
	return pkg.OKMeta(c, items, fiber.Map{"total": len(items)})
}

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
	if err := ValidateMounts(in.Mounts); err != nil {
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
	if err := ValidateMounts(in.Mounts); err != nil {
		return pkg.Err(c, err)
	}
	v, err := h.repo.UpdateVersion(c.Context(), id, in)
	if err != nil {
		return pkg.Err(c, err)
	}
	h.pushVersion(c, v, "")
	return pkg.OK(c, v)
}

// DeleteVersion DELETE /api/admin/versions/:id
// 删除后：先把 CM 期望态重指到剩余版本（若该部署已无版本则停止，避免孤儿期望态
// 继续补建），再仅回收被删版本的容器——不触碰同部署其他版本的实例。
func (h *Handler) DeleteVersion(c fiber.Ctx) error {
	id, err := parseID(c, "id")
	if err != nil {
		return pkg.Err(c, err)
	}
	v, err := h.repo.GetVersion(c.Context(), id)
	if err != nil {
		return pkg.Err(c, err)
	}
	if err := h.repo.DeleteVersion(c.Context(), id); err != nil {
		return pkg.Err(c, err)
	}
	h.syncDeployment(c, v.DeploymentID)
	h.reclaimVersion(c, v.DeploymentID, id)
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
