package tenant

import (
	"strconv"

	"github.com/NeoPlayful/maple-gateway/server/pkg"
	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
)

// Handler 暴露 Tenant 的 Management API。
type Handler struct {
	repo *Repository
}

// NewHandler 构造。
func NewHandler(repo *Repository) *Handler {
	return &Handler{repo: repo}
}

// List GET /api/admin/tenants
func (h *Handler) List(c fiber.Ctx) error {
	status := Status(c.Query("status", ""))
	limit, _ := strconv.Atoi(c.Query("limit", "20"))
	offset, _ := strconv.Atoi(c.Query("offset", "0"))
	if limit <= 0 || limit > 200 {
		limit = 20
	}
	items, total, err := h.repo.List(c.Context(), status, limit, offset)
	if err != nil {
		return pkg.Err(c, err)
	}
	return pkg.OKMeta(c, items, fiber.Map{"total": total, "limit": limit, "offset": offset})
}

// Create POST /api/admin/tenants
func (h *Handler) Create(c fiber.Ctx) error {
	var in New
	if err := c.Bind().Body(&in); err != nil {
		return pkg.Err(c, pkg.ErrValidation("请求体格式错误"))
	}
	if err := pkg.ValidateStruct(in); err != nil {
		return pkg.Err(c, err)
	}
	t, err := h.repo.Create(c.Context(), in)
	if err != nil {
		return pkg.Err(c, err)
	}
	return pkg.OK(c, t)
}

// Get GET /api/admin/tenants/:id
func (h *Handler) Get(c fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return pkg.Err(c, pkg.ErrValidation("无效的租户 ID"))
	}
	t, err := h.repo.GetByID(c.Context(), id)
	if err != nil {
		return pkg.Err(c, err)
	}
	return pkg.OK(c, t)
}

// Update PATCH /api/admin/tenants/:id
func (h *Handler) Update(c fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return pkg.Err(c, pkg.ErrValidation("无效的租户 ID"))
	}
	var in Update
	if err := c.Bind().Body(&in); err != nil {
		return pkg.Err(c, pkg.ErrValidation("请求体格式错误"))
	}
	t, err := h.repo.Update(c.Context(), id, in)
	if err != nil {
		return pkg.Err(c, err)
	}
	return pkg.OK(c, t)
}

// Delete DELETE /api/admin/tenants/:id
func (h *Handler) Delete(c fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return pkg.Err(c, pkg.ErrValidation("无效的租户 ID"))
	}
	if err := h.repo.Delete(c.Context(), id); err != nil {
		return pkg.Err(c, err)
	}
	return pkg.OK(c, fiber.Map{"deleted": true})
}

// Enable POST /api/admin/tenants/:id/enable
func (h *Handler) Enable(c fiber.Ctx) error { return h.setStatus(c, StatusActive) }

// Disable POST /api/admin/tenants/:id/disable
func (h *Handler) Disable(c fiber.Ctx) error { return h.setStatus(c, StatusDisabled) }

// Suspend POST /api/admin/tenants/:id/suspend
func (h *Handler) Suspend(c fiber.Ctx) error { return h.setStatus(c, StatusSuspended) }

func (h *Handler) setStatus(c fiber.Ctx, s Status) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return pkg.Err(c, pkg.ErrValidation("无效的租户 ID"))
	}
	t, err := h.repo.SetStatus(c.Context(), id, s)
	if err != nil {
		return pkg.Err(c, err)
	}
	return pkg.OK(c, t)
}
