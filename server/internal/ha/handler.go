package ha

import (
	"github.com/NeoPlayful/maple-gateway/server/pkg"
	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
)

// Handler 暴露 Gateway 自身实例（HA）的 Management API。
type Handler struct {
	repo  *Repository
	local *Coordinator // 可空；用于标记本进程实例 ID 与 leader 状态
}

// NewHandler 构造。
func NewHandler(repo *Repository, local *Coordinator) *Handler {
	return &Handler{repo: repo, local: local}
}

// List GET /api/admin/ha/instances
func (h *Handler) List(c fiber.Ctx) error {
	items, err := h.repo.List(c.Context())
	if err != nil {
		return pkg.Err(c, err)
	}
	return pkg.OK(c, items)
}

// Get GET /api/admin/ha/instances/:id
func (h *Handler) Get(c fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return pkg.Err(c, pkg.ErrValidation("无效的 gateway 实例 ID"))
	}
	inst, err := h.repo.GetByID(c.Context(), id)
	if err != nil {
		return pkg.Err(c, err)
	}
	return pkg.OK(c, inst)
}

// Leader GET /api/admin/ha/leader —— 当前有效 leader（lease 未过期），本进程状态一并返回。
func (h *Handler) Leader(c fiber.Ctx) error {
	leaders, err := h.repo.ListLeaders(c.Context())
	if err != nil {
		return pkg.Err(c, err)
	}
	var leader *Instance
	if len(leaders) > 0 {
		leader = leaders[0]
	}
	return pkg.OK(c, fiber.Map{
		"leader":         leader,
		"local_instance": localInfo(h.local),
	})
}

func localInfo(c *Coordinator) fiber.Map {
	if c == nil {
		return fiber.Map{"instance_id": "", "is_leader": false, "enabled": false}
	}
	return fiber.Map{
		"instance_id": c.InstanceID(),
		"is_leader":   c.IsLeader(),
		"enabled":     c.enabled(),
	}
}
