package canary

import (
	"context"
	"strconv"

	"github.com/NeoPlayful/maple-gateway/server/pkg"
	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
)

// Handler 暴露 Canary 发布的 Management API。
type Handler struct {
	svc *Service
}

// NewHandler 构造。
func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
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

// List GET /api/admin/canary?service_id=&limit=&offset=
func (h *Handler) List(c fiber.Ctx) error {
	var serviceID *uuid.UUID
	if q := c.Query("service_id"); q != "" {
		id, err := uuid.Parse(q)
		if err != nil {
			return pkg.Err(c, pkg.ErrValidation("无效的 service_id"))
		}
		serviceID = &id
	}
	limit, offset := parseLimitOffset(c)
	items, total, err := h.svc.List(c.Context(), serviceID, limit, offset)
	if err != nil {
		return pkg.Err(c, err)
	}
	return pkg.OKMeta(c, items, fiber.Map{"total": total, "limit": limit, "offset": offset})
}

// Create POST /api/admin/canary
func (h *Handler) Create(c fiber.Ctx) error {
	var in NewRelease
	if err := c.Bind().Body(&in); err != nil {
		return pkg.Err(c, pkg.ErrValidation("请求体格式错误"))
	}
	if err := pkg.ValidateStruct(in); err != nil {
		return pkg.Err(c, err)
	}
	rel, err := h.svc.Create(c.Context(), in)
	if err != nil {
		return pkg.Err(c, err)
	}
	return pkg.OK(c, rel)
}

// Get GET /api/admin/canary/:id
func (h *Handler) Get(c fiber.Ctx) error {
	id, err := parseID(c, "id")
	if err != nil {
		return pkg.Err(c, err)
	}
	rel, err := h.svc.Get(c.Context(), id)
	if err != nil {
		return pkg.Err(c, err)
	}
	return pkg.OK(c, rel)
}

// Update PATCH /api/admin/canary/:id
func (h *Handler) Update(c fiber.Ctx) error {
	id, err := parseID(c, "id")
	if err != nil {
		return pkg.Err(c, err)
	}
	var in UpdateRelease
	if err := c.Bind().Body(&in); err != nil {
		return pkg.Err(c, pkg.ErrValidation("请求体格式错误"))
	}
	rel, err := h.svc.Update(c.Context(), id, in)
	if err != nil {
		return pkg.Err(c, err)
	}
	return pkg.OK(c, rel)
}

// Delete DELETE /api/admin/canary/:id
func (h *Handler) Delete(c fiber.Ctx) error {
	id, err := parseID(c, "id")
	if err != nil {
		return pkg.Err(c, err)
	}
	if err := h.svc.Delete(c.Context(), id); err != nil {
		return pkg.Err(c, err)
	}
	return pkg.OK(c, fiber.Map{"deleted": true})
}

// Start POST /api/admin/canary/:id/start
func (h *Handler) Start(c fiber.Ctx) error { return h.action(c, h.svc.Start) }

// Pause POST /api/admin/canary/:id/pause
func (h *Handler) Pause(c fiber.Ctx) error { return h.action(c, h.svc.Pause) }

// Resume POST /api/admin/canary/:id/resume
func (h *Handler) Resume(c fiber.Ctx) error { return h.action(c, h.svc.Resume) }

// Promote POST /api/admin/canary/:id/promote
func (h *Handler) Promote(c fiber.Ctx) error { return h.action(c, h.svc.Promote) }

// Rollback POST /api/admin/canary/:id/rollback
func (h *Handler) Rollback(c fiber.Ctx) error { return h.action(c, h.svc.Rollback) }

// SetWeight POST /api/admin/canary/:id/weight
func (h *Handler) SetWeight(c fiber.Ctx) error {
	id, err := parseID(c, "id")
	if err != nil {
		return pkg.Err(c, err)
	}
	var in WeightInput
	if err := c.Bind().Body(&in); err != nil {
		return pkg.Err(c, pkg.ErrValidation("请求体格式错误"))
	}
	rel, err := h.svc.SetWeight(c.Context(), id, in.Weight)
	if err != nil {
		return pkg.Err(c, err)
	}
	return pkg.OK(c, rel)
}

type actionFn func(ctx context.Context, id uuid.UUID) (*Release, error)

func (h *Handler) action(c fiber.Ctx, fn actionFn) error {
	id, err := parseID(c, "id")
	if err != nil {
		return pkg.Err(c, err)
	}
	rel, err := fn(c.Context(), id)
	if err != nil {
		return pkg.Err(c, err)
	}
	return pkg.OK(c, rel)
}
