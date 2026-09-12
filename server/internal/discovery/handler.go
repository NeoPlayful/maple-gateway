package discovery

import (
	"github.com/NeoPlayful/maple-gateway/server/internal/instance"
	"github.com/NeoPlayful/maple-gateway/server/internal/node"
	"github.com/NeoPlayful/maple-gateway/server/pkg"
	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
)

// Handler 暴露发现 Internal API。
type Handler struct {
	nodes     *node.Repository
	instances *instance.Repository
}

// NewHandler 构造。
func NewHandler(nodes *node.Repository, instances *instance.Repository) *Handler {
	return &Handler{nodes: nodes, instances: instances}
}

// RegisterNode POST /api/internal/discovery/nodes/register
func (h *Handler) RegisterNode(c fiber.Ctx) error {
	var in node.New
	if err := c.Bind().Body(&in); err != nil {
		return pkg.Err(c, pkg.ErrValidation("请求体格式错误"))
	}
	if err := pkg.ValidateStruct(in); err != nil {
		return pkg.Err(c, err)
	}
	n, err := h.nodes.Create(c.Context(), in)
	if err != nil {
		return pkg.Err(c, err)
	}
	// 注册即心跳，避免首次注册被 watchdog 立即判 offline。
	n, err = h.nodes.Heartbeat(c.Context(), n.ID)
	if err != nil {
		return pkg.Err(c, err)
	}
	return pkg.OK(c, n)
}

// HeartbeatNode POST /api/internal/discovery/nodes/:id/heartbeat
func (h *Handler) HeartbeatNode(c fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return pkg.Err(c, pkg.ErrValidation("无效的节点 ID"))
	}
	n, err := h.nodes.Heartbeat(c.Context(), id)
	if err != nil {
		return pkg.Err(c, err)
	}
	return pkg.OK(c, n)
}

// UpdateNode PATCH /api/internal/nodes/:id
func (h *Handler) UpdateNode(c fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return pkg.Err(c, pkg.ErrValidation("无效的节点 ID"))
	}
	var in node.Update
	if err := c.Bind().Body(&in); err != nil {
		return pkg.Err(c, pkg.ErrValidation("请求体格式错误"))
	}
	n, err := h.nodes.Update(c.Context(), id, in)
	if err != nil {
		return pkg.Err(c, err)
	}
	return pkg.OK(c, n)
}

// DeleteNode DELETE /api/internal/nodes/:id
// 删除节点并清空其上实例的 node_id 引用（级联策略见 node.Repository.Delete）。
func (h *Handler) DeleteNode(c fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return pkg.Err(c, pkg.ErrValidation("无效的节点 ID"))
	}
	if err := h.nodes.Delete(c.Context(), id); err != nil {
		return pkg.Err(c, err)
	}
	return pkg.OK(c, fiber.Map{"deleted": true})
}

// RegisterInstance POST /api/internal/discovery/instances/register
func (h *Handler) RegisterInstance(c fiber.Ctx) error {
	var in instance.New
	if err := c.Bind().Body(&in); err != nil {
		return pkg.Err(c, pkg.ErrValidation("请求体格式错误"))
	}
	if in.Protocol == "" {
		in.Protocol = "http"
	}
	if err := pkg.ValidateStruct(in); err != nil {
		return pkg.Err(c, err)
	}
	i, err := h.instances.Create(c.Context(), in)
	if err != nil {
		return pkg.Err(c, err)
	}
	return pkg.OK(c, i)
}

// updateBody 是 Internal API 实例更新的合并请求体。
type updateBody struct {
	instance.Update
	instance.Mount
}

// UpdateInstance PATCH /api/internal/discovery/instances/:id
func (h *Handler) UpdateInstance(c fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return pkg.Err(c, pkg.ErrValidation("无效的实例 ID"))
	}
	var body updateBody
	if err := c.Bind().Body(&body); err != nil {
		return pkg.Err(c, pkg.ErrValidation("请求体格式错误"))
	}
	i, err := h.instances.Update(c.Context(), id, body.Update)
	if err != nil {
		return pkg.Err(c, err)
	}
	if body.Mount.DeploymentID != nil || body.Mount.VersionID != nil || body.Mount.NodeID != nil {
		i, err = h.instances.Mount(c.Context(), id, body.Mount)
		if err != nil {
			return pkg.Err(c, err)
		}
	}
	// 上报即被 CM 看见：刷新 last_seen_at。
	if i, err = h.instances.Heartbeat(c.Context(), id); err != nil {
		return pkg.Err(c, err)
	}
	return pkg.OK(c, i)
}

// DeleteInstance DELETE /api/internal/discovery/instances/:id
func (h *Handler) DeleteInstance(c fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return pkg.Err(c, pkg.ErrValidation("无效的实例 ID"))
	}
	if err := h.instances.Delete(c.Context(), id); err != nil {
		return pkg.Err(c, err)
	}
	return pkg.OK(c, fiber.Map{"deleted": true})
}

// ReportHealth POST /api/internal/discovery/instances/:id/health
func (h *Handler) ReportHealth(c fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return pkg.Err(c, pkg.ErrValidation("无效的实例 ID"))
	}
	var body struct {
		Health instance.Health `json:"health" validate:"required,oneof=unknown healthy unhealthy recovering"`
	}
	if err := c.Bind().Body(&body); err != nil {
		return pkg.Err(c, pkg.ErrValidation("请求体格式错误"))
	}
	i, err := h.instances.SetHealth(c.Context(), id, body.Health)
	if err != nil {
		return pkg.Err(c, err)
	}
	return pkg.OK(c, i)
}

// HeartbeatInstance POST /api/internal/instances/:id/heartbeat
func (h *Handler) HeartbeatInstance(c fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return pkg.Err(c, pkg.ErrValidation("无效的实例 ID"))
	}
	i, err := h.instances.Heartbeat(c.Context(), id)
	if err != nil {
		return pkg.Err(c, err)
	}
	return pkg.OK(c, i)
}

// DrainInstance POST /api/internal/instances/:id/drain
// 通知实例进入 draining（停止接收新流量），供 CM 删除容器前先摘流量。
func (h *Handler) DrainInstance(c fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return pkg.Err(c, pkg.ErrValidation("无效的实例 ID"))
	}
	i, err := h.instances.Drain(c.Context(), id)
	if err != nil {
		return pkg.Err(c, err)
	}
	return pkg.OK(c, i)
}
