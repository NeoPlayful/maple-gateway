package project

import (
	"strconv"

	"github.com/NeoPlayful/maple-gateway/server/pkg"
	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
)

// Handler 暴露 Project 的 Management API。
type Handler struct {
	repo *Repository
	// inst 可空；未接入 CM 时为 nil，实例化接口返回 503。
	inst *Instantiator
}

// NewHandler 构造。
func NewHandler(repo *Repository) *Handler {
	return &Handler{repo: repo}
}

// WithInstantiator 注入模板实例化器（渲染规格并落成 Application）。
func (h *Handler) WithInstantiator(inst *Instantiator) *Handler {
	h.inst = inst
	return h
}

// List GET /api/admin/projects?tenant_id=&limit=&offset=
func (h *Handler) List(c fiber.Ctx) error {
	var tenantID *uuid.UUID
	if q := c.Query("tenant_id"); q != "" {
		id, err := uuid.Parse(q)
		if err != nil {
			return pkg.Err(c, pkg.ErrValidation("无效的 tenant_id"))
		}
		tenantID = &id
	}
	limit, _ := strconv.Atoi(c.Query("limit", "20"))
	offset, _ := strconv.Atoi(c.Query("offset", "0"))
	if limit <= 0 || limit > 200 {
		limit = 20
	}
	items, total, err := h.repo.List(c.Context(), tenantID, limit, offset)
	if err != nil {
		return pkg.Err(c, err)
	}
	return pkg.OKMeta(c, items, fiber.Map{"total": total, "limit": limit, "offset": offset})
}

// Create POST /api/admin/projects
func (h *Handler) Create(c fiber.Ctx) error {
	var in New
	if err := c.Bind().Body(&in); err != nil {
		return pkg.Err(c, pkg.ErrValidation("请求体格式错误"))
	}
	if err := pkg.ValidateStruct(in); err != nil {
		return pkg.Err(c, err)
	}
	p, err := h.repo.Create(c.Context(), in)
	if err != nil {
		return pkg.Err(c, err)
	}
	return pkg.OK(c, p)
}

// Get GET /api/admin/projects/:id
func (h *Handler) Get(c fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return pkg.Err(c, pkg.ErrValidation("无效的项目 ID"))
	}
	p, err := h.repo.Get(c.Context(), id)
	if err != nil {
		return pkg.Err(c, err)
	}
	return pkg.OK(c, p)
}

// Update PATCH /api/admin/projects/:id
func (h *Handler) Update(c fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return pkg.Err(c, pkg.ErrValidation("无效的项目 ID"))
	}
	var in Update
	if err := c.Bind().Body(&in); err != nil {
		return pkg.Err(c, pkg.ErrValidation("请求体格式错误"))
	}
	p, err := h.repo.Update(c.Context(), id, in)
	if err != nil {
		return pkg.Err(c, err)
	}
	return pkg.OK(c, p)
}

// Delete DELETE /api/admin/projects/:id
func (h *Handler) Delete(c fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return pkg.Err(c, pkg.ErrValidation("无效的项目 ID"))
	}
	if err := h.repo.Delete(c.Context(), id); err != nil {
		return pkg.Err(c, err)
	}
	// 项目删除即归还其占用的端口；归还失败不阻断删除（记录仍在，无人再用该端口）。
	if h.inst != nil {
		_ = h.inst.ReleasePort(c.Context(), id)
	}
	return pkg.OK(c, fiber.Map{"deleted": true})
}

// Instantiate POST /api/admin/projects/:id/instantiate —— 渲染模板规格并生成 Application。
func (h *Handler) Instantiate(c fiber.Ctx) error {
	if h.inst == nil {
		return pkg.Err(c, pkg.NewAppError(503, pkg.CodeSystemError, "Container Manager 未接入"))
	}
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return pkg.Err(c, pkg.ErrValidation("无效的项目 ID"))
	}
	var in InstantiateInput
	if len(c.Body()) > 0 {
		if err := c.Bind().Body(&in); err != nil {
			return pkg.Err(c, pkg.ErrValidation("请求体格式错误"))
		}
	}
	p, err := h.inst.Instantiate(c.Context(), id, in)
	if err != nil {
		return pkg.Err(c, err)
	}
	return pkg.OK(c, p)
}

// Enable POST /api/admin/projects/:id/enable
func (h *Handler) Enable(c fiber.Ctx) error { return h.setStatus(c, StatusActive) }

// Disable POST /api/admin/projects/:id/disable
func (h *Handler) Disable(c fiber.Ctx) error { return h.setStatus(c, StatusDisabled) }

func (h *Handler) setStatus(c fiber.Ctx, s Status) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return pkg.Err(c, pkg.ErrValidation("无效的项目 ID"))
	}
	p, err := h.repo.SetStatus(c.Context(), id, s)
	if err != nil {
		return pkg.Err(c, err)
	}
	return pkg.OK(c, p)
}
