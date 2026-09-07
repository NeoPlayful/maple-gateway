package domain

import (
	"strconv"

	"github.com/NeoPlayful/maple-gateway/server/internal/router"
	"github.com/NeoPlayful/maple-gateway/server/pkg"
	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
)

// Handler 暴露 Domain 的 Management API。
type Handler struct {
	repo *Repository
}

// NewHandler 构造。
func NewHandler(repo *Repository) *Handler {
	return &Handler{repo: repo}
}

// List GET /api/admin/domains?tenant_id=&limit=&offset=
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

// Create POST /api/admin/domains
func (h *Handler) Create(c fiber.Ctx) error {
	var in New
	if err := c.Bind().Body(&in); err != nil {
		return pkg.Err(c, pkg.ErrValidation("请求体格式错误"))
	}
	norm, err := router.NormalizeHost(in.Hostname)
	if err != nil {
		return pkg.Err(c, pkg.ErrValidation("域名格式无效"))
	}
	in.Hostname = norm
	if err := pkg.ValidateStruct(in); err != nil {
		return pkg.Err(c, err)
	}
	d, err := h.repo.Create(c.Context(), in)
	if err != nil {
		return pkg.Err(c, err)
	}
	return pkg.OK(c, d)
}

// Get GET /api/admin/domains/:id
func (h *Handler) Get(c fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return pkg.Err(c, pkg.ErrValidation("无效的域名 ID"))
	}
	d, err := h.repo.GetByID(c.Context(), id)
	if err != nil {
		return pkg.Err(c, err)
	}
	return pkg.OK(c, d)
}

// Update PATCH /api/admin/domains/:id
func (h *Handler) Update(c fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return pkg.Err(c, pkg.ErrValidation("无效的域名 ID"))
	}
	var in Update
	if err := c.Bind().Body(&in); err != nil {
		return pkg.Err(c, pkg.ErrValidation("请求体格式错误"))
	}
	if in.Hostname != nil {
		n, err := router.NormalizeHost(*in.Hostname)
		if err != nil {
			return pkg.Err(c, pkg.ErrValidation("域名格式无效"))
		}
		in.Hostname = &n
	}
	d, err := h.repo.Update(c.Context(), id, in)
	if err != nil {
		return pkg.Err(c, err)
	}
	return pkg.OK(c, d)
}

// Delete DELETE /api/admin/domains/:id
func (h *Handler) Delete(c fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return pkg.Err(c, pkg.ErrValidation("无效的域名 ID"))
	}
	if err := h.repo.Delete(c.Context(), id); err != nil {
		return pkg.Err(c, err)
	}
	return pkg.OK(c, fiber.Map{"deleted": true})
}

// Enable POST /api/admin/domains/:id/enable
func (h *Handler) Enable(c fiber.Ctx) error { return h.setStatus(c, StatusActive) }

// Disable POST /api/admin/domains/:id/disable
func (h *Handler) Disable(c fiber.Ctx) error { return h.setStatus(c, StatusDisabled) }

func (h *Handler) setStatus(c fiber.Ctx, s Status) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return pkg.Err(c, pkg.ErrValidation("无效的域名 ID"))
	}
	d, err := h.repo.Update(c.Context(), id, Update{Status: &s})
	if err != nil {
		return pkg.Err(c, err)
	}
	return pkg.OK(c, d)
}
