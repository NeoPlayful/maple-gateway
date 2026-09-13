package api

import (
	"context"

	"github.com/NeoPlayful/maple-gateway/server/internal/agentprotocol"
	"github.com/NeoPlayful/maple-gateway/server/internal/containermanager/applications"
	"github.com/gofiber/fiber/v3"
)

// AppSource 提供 Compose 应用的增删改查与部署控制。
// 部署类方法带 ctx（需经节点命令通道下发）；本地读写方法不带。
type AppSource struct {
	List     func() []applications.Application
	Get      func(id string) (applications.Application, bool)
	Put      func(a applications.Application) applications.Application
	Delete   func(id string)
	Deploy   func(ctx context.Context, id string) (applications.Application, error)
	Stop     func(ctx context.Context, id string) (applications.Application, error)
	Start    func(ctx context.Context, id string) (applications.Application, error)
	Restart  func(ctx context.Context, id string) (applications.Application, error)
	Remove   func(ctx context.Context, id string) error
	Ps       func(ctx context.Context, id string) ([]agentprotocol.ApplicationService, error)
	Validate func(ctx context.Context, id string) (bool, string, error)
}

// registerApplications 挂载 Compose 应用管理接口（令牌认证，供 Gateway 聚合代理调用）。
func registerApplications(app *fiber.App, token string, a AppSource) {
	g := app.Group("/api/mgmt/applications", gatewayAuth(token))

	// GET /api/mgmt/applications 列出全部应用。
	g.Get("/", func(c fiber.Ctx) error {
		if a.List == nil {
			return c.JSON([]applications.Application{})
		}
		return c.JSON(a.List())
	})

	// GET /api/mgmt/applications/:id 应用详情。
	g.Get("/:id", func(c fiber.Ctx) error {
		if a.Get == nil {
			return fiber.NewError(fiber.StatusNotFound, "not found")
		}
		app, ok := a.Get(c.Params("id"))
		if !ok {
			return fiber.NewError(fiber.StatusNotFound, "应用不存在")
		}
		return c.JSON(app)
	})

	// POST /api/mgmt/applications 新建/覆盖应用（携带 Compose 规格）。
	g.Post("/", func(c fiber.Ctx) error {
		if a.Put == nil {
			return fiber.NewError(fiber.StatusNotImplemented, "未启用应用管理")
		}
		var in applications.Application
		if err := c.Bind().Body(&in); err != nil {
			return fiber.NewError(fiber.StatusBadRequest, "请求体格式错误")
		}
		if in.Name == "" {
			return fiber.NewError(fiber.StatusBadRequest, "缺少应用名")
		}
		if in.Spec == "" {
			return fiber.NewError(fiber.StatusBadRequest, "缺少 Compose 规格")
		}
		out := a.Put(in)
		return c.JSON(out)
	})

	// DELETE /api/mgmt/applications/:id 移除应用（先 compose down 再删记录）。
	g.Delete("/:id", func(c fiber.Ctx) error {
		if a.Remove == nil {
			return fiber.NewError(fiber.StatusNotImplemented, "未启用应用管理")
		}
		if err := a.Remove(c.Context(), c.Params("id")); err != nil {
			return fiber.NewError(fiber.StatusBadGateway, err.Error())
		}
		return c.JSON(fiber.Map{"removed": true})
	})

	// POST /api/mgmt/applications/:id/deploy 部署。
	g.Post("/:id/deploy", func(c fiber.Ctx) error {
		if a.Deploy == nil {
			return fiber.NewError(fiber.StatusNotImplemented, "未启用应用部署")
		}
		out, err := a.Deploy(c.Context(), c.Params("id"))
		if err != nil {
			return fiber.NewError(fiber.StatusBadGateway, err.Error())
		}
		return c.JSON(out)
	})

	// POST /api/mgmt/applications/:id/stop 停止。
	g.Post("/:id/stop", func(c fiber.Ctx) error {
		if a.Stop == nil {
			return fiber.NewError(fiber.StatusNotImplemented, "未启用应用管理")
		}
		out, err := a.Stop(c.Context(), c.Params("id"))
		if err != nil {
			return fiber.NewError(fiber.StatusBadGateway, err.Error())
		}
		return c.JSON(out)
	})

	// POST /api/mgmt/applications/:id/start 启动（复用既有容器）。
	g.Post("/:id/start", func(c fiber.Ctx) error {
		if a.Start == nil {
			return fiber.NewError(fiber.StatusNotImplemented, "未启用应用管理")
		}
		out, err := a.Start(c.Context(), c.Params("id"))
		if err != nil {
			return fiber.NewError(fiber.StatusBadGateway, err.Error())
		}
		return c.JSON(out)
	})

	// POST /api/mgmt/applications/:id/restart 重启。
	g.Post("/:id/restart", func(c fiber.Ctx) error {
		if a.Restart == nil {
			return fiber.NewError(fiber.StatusNotImplemented, "未启用应用管理")
		}
		out, err := a.Restart(c.Context(), c.Params("id"))
		if err != nil {
			return fiber.NewError(fiber.StatusBadGateway, err.Error())
		}
		return c.JSON(out)
	})

	// GET /api/mgmt/applications/:id/ps 应用内服务运行态。
	g.Get("/:id/ps", func(c fiber.Ctx) error {
		if a.Ps == nil {
			return c.JSON(fiber.Map{"services": []any{}})
		}
		svcs, err := a.Ps(c.Context(), c.Params("id"))
		if err != nil {
			return fiber.NewError(fiber.StatusBadGateway, err.Error())
		}
		return c.JSON(fiber.Map{"services": svcs})
	})

	// POST /api/mgmt/applications/:id/validate 校验 Compose 规格。
	g.Post("/:id/validate", func(c fiber.Ctx) error {
		if a.Validate == nil {
			return fiber.NewError(fiber.StatusNotImplemented, "未启用应用校验")
		}
		valid, output, err := a.Validate(c.Context(), c.Params("id"))
		if err != nil {
			return fiber.NewError(fiber.StatusBadGateway, err.Error())
		}
		return c.JSON(fiber.Map{"valid": valid, "output": output})
	})
}
