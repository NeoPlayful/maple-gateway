package api

import (
	"strings"

	"github.com/NeoPlayful/maple-gateway/server/internal/containermanager/desired"
	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
)

// internalAuth 校验 Gateway 下发的内部令牌（Authorization: Bearer <cm.token>）。
func internalAuth(token string) fiber.Handler {
	return func(c fiber.Ctx) error {
		if token == "" {
			return fiber.NewError(fiber.StatusUnauthorized, "cm token 未配置")
		}
		h := c.Get("Authorization")
		if !strings.HasPrefix(h, "Bearer ") || strings.TrimPrefix(h, "Bearer ") != token {
			return fiber.NewError(fiber.StatusUnauthorized, "无效的 CM token")
		}
		return c.Next()
	}
}

// registerDeployments 挂载部署意图接收与状态查询路由（内部令牌认证）。
func registerDeployments(app *fiber.App, store *desired.Store, token string) {
	g := app.Group("/api/internal/deployments", internalAuth(token))

	// POST /api/internal/deployments 下发/更新部署期望态。
	g.Post("/", func(c fiber.Ctx) error {
		var in desired.State
		if err := c.Bind().Body(&in); err != nil {
			return fiber.NewError(fiber.StatusBadRequest, "请求体格式错误")
		}
		if in.DeploymentID == uuid.Nil || in.Image == "" {
			return fiber.NewError(fiber.StatusBadRequest, "缺少 deployment_id 或 image")
		}
		store.Put(in)
		return c.JSON(in)
	})

	// POST /api/internal/deployments/:id/stop 停止部署。
	g.Post("/:id/stop", func(c fiber.Ctx) error {
		id, err := uuid.Parse(c.Params("id"))
		if err != nil {
			return fiber.NewError(fiber.StatusBadRequest, "无效的 deployment_id")
		}
		store.Stop(id)
		return c.JSON(fiber.Map{"stopped": true})
	})

	// GET /api/internal/deployments/:id/status 查询编排进度。
	g.Get("/:id/status", func(c fiber.Ctx) error {
		id, err := uuid.Parse(c.Params("id"))
		if err != nil {
			return fiber.NewError(fiber.StatusBadRequest, "无效的 deployment_id")
		}
		return c.JSON(store.Phase(id))
	})
}

// registerStats 挂载 CM 集成健康统计端点（供监控查询上报滞后/错误/纳管数）。
func registerStats(app *fiber.App, stats StatsProvider) {
	app.Get("/api/internal/stats", func(c fiber.Ctx) error {
		return c.JSON(stats.Stats())
	})
}
