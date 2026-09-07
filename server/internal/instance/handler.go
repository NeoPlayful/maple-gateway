package instance

import (
	"strconv"

	"github.com/NeoPlayful/maple-gateway/server/internal/security"
	"github.com/NeoPlayful/maple-gateway/server/pkg"
	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
)

// Handler 暴露 Instance 的 Management API。
type Handler struct {
	repo *Repository
}

// NewHandler 构造。
func NewHandler(repo *Repository) *Handler {
	return &Handler{repo: repo}
}

// List GET /api/admin/instances?service_id=&limit=&offset=
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

// Get GET /api/admin/instances/:id
func (h *Handler) Get(c fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return pkg.Err(c, pkg.ErrValidation("无效的实例 ID"))
	}
	i, err := h.repo.GetByID(c.Context(), id)
	if err != nil {
		return pkg.Err(c, err)
	}
	return pkg.OK(c, i)
}

// Register POST /api/admin/instances/register
func (h *Handler) Register(c fiber.Ctx) error {
	var in New
	if err := c.Bind().Body(&in); err != nil {
		return pkg.Err(c, pkg.ErrValidation("请求体格式错误"))
	}
	if in.Protocol == "" {
		in.Protocol = "http"
	}
	if err := pkg.ValidateStruct(in); err != nil {
		return pkg.Err(c, err)
	}
	if err := security.ValidateUpstreamAddress(in.Address); err != nil {
		return pkg.Err(c, err)
	}
	i, err := h.repo.Create(c.Context(), in)
	if err != nil {
		return pkg.Err(c, err)
	}
	return pkg.OK(c, i)
}

// Update PATCH /api/admin/instances/:id
func (h *Handler) Update(c fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return pkg.Err(c, pkg.ErrValidation("无效的实例 ID"))
	}
	var in Update
	if err := c.Bind().Body(&in); err != nil {
		return pkg.Err(c, pkg.ErrValidation("请求体格式错误"))
	}
	i, err := h.repo.Update(c.Context(), id, in)
	if err != nil {
		return pkg.Err(c, err)
	}
	return pkg.OK(c, i)
}

// Delete DELETE /api/admin/instances/:id
func (h *Handler) Delete(c fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return pkg.Err(c, pkg.ErrValidation("无效的实例 ID"))
	}
	if err := h.repo.Delete(c.Context(), id); err != nil {
		return pkg.Err(c, err)
	}
	return pkg.OK(c, fiber.Map{"deleted": true})
}

// Enable POST /api/admin/instances/:id/enable
func (h *Handler) Enable(c fiber.Ctx) error { return h.setStatus(c, StatusEnabled) }

// Disable POST /api/admin/instances/:id/disable
func (h *Handler) Disable(c fiber.Ctx) error { return h.setStatus(c, StatusDisabled) }

// Drain POST /api/admin/instances/:id/drain
func (h *Handler) Drain(c fiber.Ctx) error { return h.setStatus(c, StatusDraining) }

// Undrain POST /api/admin/instances/:id/undrain
func (h *Handler) Undrain(c fiber.Ctx) error { return h.setStatus(c, StatusEnabled) }

func (h *Handler) setStatus(c fiber.Ctx, s Status) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return pkg.Err(c, pkg.ErrValidation("无效的实例 ID"))
	}
	i, err := h.repo.Update(c.Context(), id, Update{Status: &s})
	if err != nil {
		return pkg.Err(c, err)
	}
	return pkg.OK(c, i)
}

// Health POST /api/admin/instances/:id/health  手动上报健康状态
func (h *Handler) Health(c fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return pkg.Err(c, pkg.ErrValidation("无效的实例 ID"))
	}
	var body struct {
		Health Health `json:"health" validate:"required,oneof=unknown healthy unhealthy recovering"`
	}
	if err := c.Bind().Body(&body); err != nil {
		return pkg.Err(c, pkg.ErrValidation("请求体格式错误"))
	}
	i, err := h.repo.SetHealth(c.Context(), id, body.Health)
	if err != nil {
		return pkg.Err(c, err)
	}
	return pkg.OK(c, i)
}
