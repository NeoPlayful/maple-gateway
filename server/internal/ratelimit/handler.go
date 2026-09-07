package ratelimit

import (
	"strconv"

	"github.com/NeoPlayful/maple-gateway/server/pkg"
	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
)

// Handler 暴露限流规则的 Management API。
type Handler struct {
	repo *Repository
}

// NewHandler 构造。
func NewHandler(repo *Repository) *Handler {
	return &Handler{repo: repo}
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

// List GET /api/admin/rate-limits?scope=&limit=&offset=
func (h *Handler) List(c fiber.Ctx) error {
	limit, offset := parseLimitOffset(c)
	items, total, err := h.repo.List(c.Context(), Scope(c.Query("scope")), limit, offset)
	if err != nil {
		return pkg.Err(c, err)
	}
	return pkg.OKMeta(c, items, fiber.Map{"total": total, "limit": limit, "offset": offset})
}

// Create POST /api/admin/rate-limits
func (h *Handler) Create(c fiber.Ctx) error {
	var in NewRateLimit
	if err := c.Bind().Body(&in); err != nil {
		return pkg.Err(c, pkg.ErrValidation("请求体格式错误"))
	}
	if err := pkg.ValidateStruct(in); err != nil {
		return pkg.Err(c, err)
	}
	rl, err := h.repo.Create(c.Context(), in)
	if err != nil {
		return pkg.Err(c, err)
	}
	return pkg.OK(c, rl)
}

// Get GET /api/admin/rate-limits/:id
func (h *Handler) Get(c fiber.Ctx) error {
	id, err := parseID(c, "id")
	if err != nil {
		return pkg.Err(c, err)
	}
	rl, err := h.repo.Get(c.Context(), id)
	if err != nil {
		return pkg.Err(c, err)
	}
	return pkg.OK(c, rl)
}

// Update PATCH /api/admin/rate-limits/:id
func (h *Handler) Update(c fiber.Ctx) error {
	id, err := parseID(c, "id")
	if err != nil {
		return pkg.Err(c, err)
	}
	var in UpdateRateLimit
	if err := c.Bind().Body(&in); err != nil {
		return pkg.Err(c, pkg.ErrValidation("请求体格式错误"))
	}
	rl, err := h.repo.Update(c.Context(), id, in)
	if err != nil {
		return pkg.Err(c, err)
	}
	return pkg.OK(c, rl)
}

// Delete DELETE /api/admin/rate-limits/:id
func (h *Handler) Delete(c fiber.Ctx) error {
	id, err := parseID(c, "id")
	if err != nil {
		return pkg.Err(c, err)
	}
	if err := h.repo.Delete(c.Context(), id); err != nil {
		return pkg.Err(c, err)
	}
	return pkg.OK(c, fiber.Map{"deleted": true})
}

// Enable POST /api/admin/rate-limits/:id/enable
func (h *Handler) Enable(c fiber.Ctx) error { return h.setStatus(c, StatusEnabled) }

// Disable POST /api/admin/rate-limits/:id/disable
func (h *Handler) Disable(c fiber.Ctx) error { return h.setStatus(c, StatusDisabled) }

func (h *Handler) setStatus(c fiber.Ctx, s Status) error {
	id, err := parseID(c, "id")
	if err != nil {
		return pkg.Err(c, err)
	}
	rl, err := h.repo.SetStatus(c.Context(), id, s)
	if err != nil {
		return pkg.Err(c, err)
	}
	return pkg.OK(c, rl)
}
