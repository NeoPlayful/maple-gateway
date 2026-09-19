package api

import (
	"context"
	"strings"

	"github.com/NeoPlayful/maple-gateway/server/internal/containermanager/desired"
	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
)

// gatewayAuth 校验 Gateway 下发的令牌（Authorization: Bearer <cm.token>）。
func gatewayAuth(token string) fiber.Handler {
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

// registerDeployments 挂载部署意图接收与状态查询路由（令牌认证）。
// removeDeployment 可空：停止部署时用它回收该部署名下已存在的容器，
// 与「停止即摘除容器」的语义对齐，避免只停补建而残留运行中的容器。
// removeVersion 可空：删除版本时用它仅回收该版本的容器，不波及其它版本。
func registerDeployments(app *fiber.App, store *desired.Store, token string, removeDeployment func(ctx context.Context, deploymentID string) int, removeVersion func(ctx context.Context, deploymentID, versionID string) int) {
	g := app.Group("/api/deployments", gatewayAuth(token))

	// POST /api/deployments 下发/更新部署期望态。
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

	// POST /api/deployments/:id/stop 停止部署：摘除期望态并回收已存在的容器。
	g.Post("/:id/stop", func(c fiber.Ctx) error {
		id, err := uuid.Parse(c.Params("id"))
		if err != nil {
			return fiber.NewError(fiber.StatusBadRequest, "无效的 deployment_id")
		}
		// 先摘期望态：此后对账器不再为该部署补建副本。
		store.Stop(id)
		// 再回收已存在的容器：期望态已移除，删掉的不会被杀回来。
		removed := 0
		if removeDeployment != nil {
			removed = removeDeployment(c.Context(), id.String())
		}
		return c.JSON(fiber.Map{"stopped": true, "removed": removed})
	})

	// POST /api/deployments/:id/forget 清除期望态：仅摘除期望态，不回收已存在的容器。
	// 与 /stop 的区别在于不删容器——用于「删除版本后该部署已无可下发版本，但幸存版本的
	// 容器须保留」的收敛场景：清掉指向被删版本的孤儿期望态，避免对账器继续补建，
	// 同时不动其它版本仍在运行的容器。
	g.Post("/:id/forget", func(c fiber.Ctx) error {
		id, err := uuid.Parse(c.Params("id"))
		if err != nil {
			return fiber.NewError(fiber.StatusBadRequest, "无效的 deployment_id")
		}
		store.Stop(id)
		return c.JSON(fiber.Map{"forgotten": true})
	})

	// POST /api/deployments/:id/versions/:vid/remove 删除版本：仅回收该版本的容器。
	// 期望态按部署唯一（cm_deployments 主键为 deployment_id），版本级删除不摘期望态，
	// 由 Gateway 侧重新下发剩余版本覆盖；此端点只做「回收这一版容器」这件事。
	g.Post("/:id/versions/:vid/remove", func(c fiber.Ctx) error {
		id, err := uuid.Parse(c.Params("id"))
		if err != nil {
			return fiber.NewError(fiber.StatusBadRequest, "无效的 deployment_id")
		}
		vid, err := uuid.Parse(c.Params("vid"))
		if err != nil {
			return fiber.NewError(fiber.StatusBadRequest, "无效的 version_id")
		}
		removed := 0
		if removeVersion != nil {
			removed = removeVersion(c.Context(), id.String(), vid.String())
		}
		return c.JSON(fiber.Map{"removed": removed})
	})

	// GET /api/deployments/:id/status 查询编排进度。
	g.Get("/:id/status", func(c fiber.Ctx) error {
		id, err := uuid.Parse(c.Params("id"))
		if err != nil {
			return fiber.NewError(fiber.StatusBadRequest, "无效的 deployment_id")
		}
		return c.JSON(store.Phase(id))
	})
}

// registerStats 挂载 CM 集成健康统计端点（供监控查询上报滞后/错误/纳管数，令牌认证）。
func registerStats(app *fiber.App, token string, stats StatsProvider) {
	app.Get("/api/stats", gatewayAuth(token), func(c fiber.Ctx) error {
		return c.JSON(stats.Stats())
	})
}
