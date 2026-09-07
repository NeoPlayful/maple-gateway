package traffic

import (
	"strconv"

	"github.com/NeoPlayful/maple-gateway/server/pkg"
	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
)

// Handler 暴露流量策略 Management API。
type Handler struct {
	repo *Repository
}

// NewHandler 构造。
func NewHandler(repo *Repository) *Handler {
	return &Handler{repo: repo}
}

// List GET /api/admin/traffic?service_id=&limit=&offset=
func (h *Handler) List(c fiber.Ctx) error {
	svcParam := c.Query("service_id")
	if svcParam != "" {
		sid, err := uuid.Parse(svcParam)
		if err != nil {
			return pkg.Err(c, pkg.ErrValidation("无效的 service_id"))
		}
		items, err := h.repo.ListByService(c.Context(), sid)
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
	items, total, err := h.repo.ListAll(c.Context(), limit, offset)
	if err != nil {
		return pkg.Err(c, err)
	}
	return pkg.OKMeta(c, items, fiber.Map{"total": total, "limit": limit, "offset": offset})
}

// Create POST /api/admin/traffic
func (h *Handler) Create(c fiber.Ctx) error {
	var in NewInput
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

// Get GET /api/admin/traffic/:id
func (h *Handler) Get(c fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return pkg.Err(c, pkg.ErrValidation("无效的策略 ID"))
	}
	p, err := h.repo.GetByID(c.Context(), id)
	if err != nil {
		return pkg.Err(c, err)
	}
	return pkg.OK(c, p)
}

// Update PATCH /api/admin/traffic/:id
func (h *Handler) Update(c fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return pkg.Err(c, pkg.ErrValidation("无效的策略 ID"))
	}
	var in UpdateInput
	if err := c.Bind().Body(&in); err != nil {
		return pkg.Err(c, pkg.ErrValidation("请求体格式错误"))
	}
	p, err := h.repo.Update(c.Context(), id, in)
	if err != nil {
		return pkg.Err(c, err)
	}
	return pkg.OK(c, p)
}

// Delete DELETE /api/admin/traffic/:id
func (h *Handler) Delete(c fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return pkg.Err(c, pkg.ErrValidation("无效的策略 ID"))
	}
	if err := h.repo.Delete(c.Context(), id); err != nil {
		return pkg.Err(c, err)
	}
	return pkg.OK(c, fiber.Map{"deleted": true})
}

// Enable POST /api/admin/traffic/:id/enable
func (h *Handler) Enable(c fiber.Ctx) error { return h.setStatus(c, StatusEnabled) }

// Disable POST /api/admin/traffic/:id/disable
func (h *Handler) Disable(c fiber.Ctx) error { return h.setStatus(c, StatusDisabled) }

func (h *Handler) setStatus(c fiber.Ctx, s Status) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return pkg.Err(c, pkg.ErrValidation("无效的策略 ID"))
	}
	p, err := h.repo.SetStatus(c.Context(), id, s)
	if err != nil {
		return pkg.Err(c, err)
	}
	return pkg.OK(c, p)
}
