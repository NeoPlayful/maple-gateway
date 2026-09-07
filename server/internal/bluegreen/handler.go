package bluegreen

import (
	"strconv"

	"github.com/NeoPlayful/maple-gateway/server/pkg"
	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
)

// Handler 暴露 Blue/Green 的 Management API。
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

// List GET /api/admin/blue-green?deployment_id=&limit=&offset=
func (h *Handler) List(c fiber.Ctx) error {
	var depID *uuid.UUID
	if q := c.Query("deployment_id"); q != "" {
		id, err := uuid.Parse(q)
		if err != nil {
			return pkg.Err(c, pkg.ErrValidation("无效的 deployment_id"))
		}
		depID = &id
	}
	limit, offset := parseLimitOffset(c)
	items, total, err := h.svc.List(c.Context(), depID, limit, offset)
	if err != nil {
		return pkg.Err(c, err)
	}
	return pkg.OKMeta(c, items, fiber.Map{"total": total, "limit": limit, "offset": offset})
}

// Create POST /api/admin/blue-green
func (h *Handler) Create(c fiber.Ctx) error {
	var in NewBG
	if err := c.Bind().Body(&in); err != nil {
		return pkg.Err(c, pkg.ErrValidation("请求体格式错误"))
	}
	if err := pkg.ValidateStruct(in); err != nil {
		return pkg.Err(c, err)
	}
	bg, err := h.svc.Create(c.Context(), in)
	if err != nil {
		return pkg.Err(c, err)
	}
	// 使 initial active 立即生效（第二个事务：登记成功即翻转角色）。
	bg, err = h.svc.ApplyInitial(c.Context(), bg.ID)
	if err != nil {
		return pkg.Err(c, err)
	}
	return pkg.OK(c, bg)
}

// Get GET /api/admin/blue-green/:id
func (h *Handler) Get(c fiber.Ctx) error {
	id, err := parseID(c, "id")
	if err != nil {
		return pkg.Err(c, err)
	}
	bg, err := h.svc.Get(c.Context(), id)
	if err != nil {
		return pkg.Err(c, err)
	}
	return pkg.OK(c, bg)
}

// Delete DELETE /api/admin/blue-green/:id
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

// switchBody 切换目标。
type switchBody struct {
	TargetVersionID uuid.UUID `json:"target_version_id" validate:"required"`
}

// Switch POST /api/admin/blue-green/:id/switch
func (h *Handler) Switch(c fiber.Ctx) error {
	id, err := parseID(c, "id")
	if err != nil {
		return pkg.Err(c, err)
	}
	var in switchBody
	if err := c.Bind().Body(&in); err != nil {
		return pkg.Err(c, pkg.ErrValidation("请求体格式错误"))
	}
	bg, err := h.svc.Switch(c.Context(), id, in.TargetVersionID)
	if err != nil {
		return pkg.Err(c, err)
	}
	return pkg.OK(c, bg)
}

// Rollback POST /api/admin/blue-green/:id/rollback
func (h *Handler) Rollback(c fiber.Ctx) error {
	id, err := parseID(c, "id")
	if err != nil {
		return pkg.Err(c, err)
	}
	bg, err := h.svc.Rollback(c.Context(), id)
	if err != nil {
		return pkg.Err(c, err)
	}
	return pkg.OK(c, bg)
}
