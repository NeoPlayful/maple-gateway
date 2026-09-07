package service

import (
	"strconv"

	"github.com/NeoPlayful/maple-gateway/server/pkg"
	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
)

// Handler 暴露 Service 的 Management API。
type Handler struct {
	repo *Repository
}

// NewHandler 构造。
func NewHandler(repo *Repository) *Handler {
	return &Handler{repo: repo}
}

// List GET /api/admin/services?tenant_id=&limit=&offset=
func (h *Handler) List(c fiber.Ctx) error {
	tenantParam := c.Query("tenant_id")
	if tenantParam != "" {
		tid, err := uuid.Parse(tenantParam)
		if err != nil {
			return pkg.Err(c, pkg.ErrValidation("无效的 tenant_id"))
		}
		items, err := h.repo.ListByTenant(c.Context(), tid)
		if err != nil {
			return pkg.Err(c, err)
		}
		return pkg.OKMeta(c, items, fiber.Map{"total": len(items)})
	}
	limit, _ := strconv.Atoi(c.Query("limit", "20"))
	offset, _ := strconv.Atoi(c.Query("offset", "0"))
	if limit <= 0 || limit > 200 {
		limit = 20
	}
	items, total, err := h.repo.List(c.Context(), limit, offset)
	if err != nil {
		return pkg.Err(c, err)
	}
	return pkg.OKMeta(c, items, fiber.Map{"total": total, "limit": limit, "offset": offset})
}

// Create POST /api/admin/services
func (h *Handler) Create(c fiber.Ctx) error {
	var in New
	if err := c.Bind().Body(&in); err != nil {
		return pkg.Err(c, pkg.ErrValidation("请求体格式错误"))
	}
	if err := pkg.ValidateStruct(in); err != nil {
		return pkg.Err(c, err)
	}
	s, err := h.repo.Create(c.Context(), in)
	if err != nil {
		return pkg.Err(c, err)
	}
	return pkg.OK(c, s)
}

// Get GET /api/admin/services/:id
func (h *Handler) Get(c fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return pkg.Err(c, pkg.ErrValidation("无效的服务 ID"))
	}
	s, err := h.repo.GetByID(c.Context(), id)
	if err != nil {
		return pkg.Err(c, err)
	}
	return pkg.OK(c, s)
}

// Update PATCH /api/admin/services/:id
func (h *Handler) Update(c fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return pkg.Err(c, pkg.ErrValidation("无效的服务 ID"))
	}
	var in Update
	if err := c.Bind().Body(&in); err != nil {
		return pkg.Err(c, pkg.ErrValidation("请求体格式错误"))
	}
	s, err := h.repo.Update(c.Context(), id, in)
	if err != nil {
		return pkg.Err(c, err)
	}
	return pkg.OK(c, s)
}

// Delete DELETE /api/admin/services/:id
func (h *Handler) Delete(c fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return pkg.Err(c, pkg.ErrValidation("无效的服务 ID"))
	}
	if err := h.repo.Delete(c.Context(), id); err != nil {
		return pkg.Err(c, err)
	}
	return pkg.OK(c, fiber.Map{"deleted": true})
}

// Enable POST /api/admin/services/:id/enable
func (h *Handler) Enable(c fiber.Ctx) error { return h.setStatus(c, StatusActive) }

// Disable POST /api/admin/services/:id/disable
func (h *Handler) Disable(c fiber.Ctx) error { return h.setStatus(c, StatusDisabled) }

func (h *Handler) setStatus(c fiber.Ctx, s Status) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return pkg.Err(c, pkg.ErrValidation("无效的服务 ID"))
	}
	srv, err := h.repo.Update(c.Context(), id, Update{Status: &s})
	if err != nil {
		return pkg.Err(c, err)
	}
	return pkg.OK(c, srv)
}
