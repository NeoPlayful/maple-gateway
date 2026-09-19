package api

import (
	"github.com/NeoPlayful/maple-gateway/server/internal/containermanager/netpools"
	"github.com/gofiber/fiber/v3"
)

// registerNetPools 挂载项目级 IP 池的管理接口（令牌认证，供 Gateway 聚合代理调用）。
func registerNetPools(app *fiber.App, token string, m Mgmt) {
	g := app.Group("/api/mgmt", gatewayAuth(token))

	// GET /api/mgmt/nodes/:nodeId/network-pools 列出某节点的网络池（含容量/用量）。
	g.Get("/nodes/:nodeId/network-pools", func(c fiber.Ctx) error {
		if m.NetworkPools == nil {
			return c.JSON([]netpools.PoolView{})
		}
		return c.JSON(m.NetworkPools(c.Params("nodeId")))
	})

	// POST /api/mgmt/nodes/:nodeId/network-pools 新增网络池。
	g.Post("/nodes/:nodeId/network-pools", func(c fiber.Ctx) error {
		if m.CreateNetworkPool == nil {
			return fiber.NewError(fiber.StatusNotImplemented, "未启用网络池管理")
		}
		var in netpools.CreatePoolInput
		if err := c.Bind().Body(&in); err != nil {
			return fiber.NewError(fiber.StatusBadRequest, "请求体格式错误")
		}
		in.NodeID = c.Params("nodeId")
		out, err := m.CreateNetworkPool(c.Context(), in)
		if err != nil {
			return fiber.NewError(fiber.StatusBadGateway, err.Error())
		}
		return c.JSON(out)
	})

	// PATCH /api/mgmt/nodes/:nodeId/network-pools/:poolId 更新池可变字段。
	g.Patch("/nodes/:nodeId/network-pools/:poolId", func(c fiber.Ctx) error {
		if m.UpdateNetworkPool == nil {
			return fiber.NewError(fiber.StatusNotImplemented, "未启用网络池管理")
		}
		var in netpools.UpdatePoolInput
		if err := c.Bind().Body(&in); err != nil {
			return fiber.NewError(fiber.StatusBadRequest, "请求体格式错误")
		}
		out, err := m.UpdateNetworkPool(c.Context(), c.Params("nodeId"), c.Params("poolId"), in)
		if err != nil {
			return fiber.NewError(fiber.StatusBadGateway, err.Error())
		}
		return c.JSON(out)
	})

	// DELETE /api/mgmt/nodes/:nodeId/network-pools/:poolId 删除池（在用则拒绝）。
	g.Delete("/nodes/:nodeId/network-pools/:poolId", func(c fiber.Ctx) error {
		if m.DeleteNetworkPool == nil {
			return fiber.NewError(fiber.StatusNotImplemented, "未启用网络池管理")
		}
		if err := m.DeleteNetworkPool(c.Context(), c.Params("nodeId"), c.Params("poolId")); err != nil {
			return fiber.NewError(fiber.StatusBadGateway, err.Error())
		}
		return c.JSON(fiber.Map{"deleted": true})
	})

	// POST /api/mgmt/nodes/:nodeId/network-pools/:poolId/check 复检冲突。
	g.Post("/nodes/:nodeId/network-pools/:poolId/check", func(c fiber.Ctx) error {
		if m.CheckNetworkPool == nil {
			return fiber.NewError(fiber.StatusNotImplemented, "未启用网络池管理")
		}
		out, err := m.CheckNetworkPool(c.Context(), c.Params("nodeId"), c.Params("poolId"))
		if err != nil {
			return fiber.NewError(fiber.StatusBadGateway, err.Error())
		}
		return c.JSON(out)
	})

	// GET /api/mgmt/projects/:projectId/network 查询项目占用的网段。
	g.Get("/projects/:projectId/network", func(c fiber.Ctx) error {
		if m.ProjectNetwork == nil {
			return fiber.NewError(fiber.StatusNotFound, "项目网络不存在")
		}
		n, ok := m.ProjectNetwork(c.Params("projectId"))
		if !ok {
			return fiber.NewError(fiber.StatusNotFound, "项目网络不存在")
		}
		return c.JSON(n)
	})
}
