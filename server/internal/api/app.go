// Package api 组装 Management API 的全部路由。
package api

import (
	"time"

	"github.com/NeoPlayful/maple-gateway/server/internal/auth"
	"github.com/NeoPlayful/maple-gateway/server/internal/cache"
	"github.com/NeoPlayful/maple-gateway/server/internal/domain"
	"github.com/NeoPlayful/maple-gateway/server/internal/instance"
	"github.com/NeoPlayful/maple-gateway/server/internal/service"
	"github.com/NeoPlayful/maple-gateway/server/internal/system"
	"github.com/NeoPlayful/maple-gateway/server/internal/tenant"
	"github.com/gofiber/fiber/v3"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Deps 是 Management API 所需依赖。
type Deps struct {
	Pool       *pgxpool.Pool // nil 表示未接入 DB（禁用 admin 与业务接口）
	RouteCache *cache.Cache  // 可空；用于 route/cache 查看与手动重建
}

// New 构造 Fiber app 并注册全部 Management API 路由。
func New(d Deps) *fiber.App {
	app := fiber.New(fiber.Config{
		AppName:     "maple-gateway-mgmt",
		BodyLimit:   4 * 1024 * 1024,
		Concurrency: 1024 * 10,
	})

	// 系统接口（无需认证）。
	sys := system.NewHandler(d.Pool)
	app.Get("/api/system/health", sys.Health)
	app.Get("/api/system/live", sys.Live)
	app.Get("/api/system/ready", sys.Ready)

	if d.Pool == nil {
		app.Get("/api/system/ready", sys.Ready)
		return app
	}

	// 认证。
	authSvc := auth.NewService(d.Pool, auth.Secret(), 24*time.Hour)
	authH := auth.NewHandler(authSvc)
	app.Post("/api/auth/login", authH.Login)

	// 需要登录的管理面路由（含审计）。
	admin := app.Group("/api/admin", auth.Middleware(authSvc), auditMiddleware(d.Pool))
	admin.Get("/system/info", sys.Info)

	// Auth me/logout 也放在认证组内。
	admin.Get("/auth/me", authH.Me)
	admin.Post("/auth/logout", authH.Logout)

	// 业务模块 CRUD。
	tenantH := tenant.NewHandler(tenant.NewRepository(d.Pool))
	t := admin.Group("/tenants")
	t.Get("/", tenantH.List)
	t.Post("/", tenantH.Create)
	t.Get("/:id", tenantH.Get)
	t.Patch("/:id", tenantH.Update)
	t.Delete("/:id", tenantH.Delete)
	t.Post("/:id/enable", tenantH.Enable)
	t.Post("/:id/disable", tenantH.Disable)
	t.Post("/:id/suspend", tenantH.Suspend)

	domainH := domain.NewHandler(domain.NewRepository(d.Pool))
	dm := admin.Group("/domains")
	dm.Get("/", domainH.List)
	dm.Post("/", domainH.Create)
	dm.Get("/:id", domainH.Get)
	dm.Patch("/:id", domainH.Update)
	dm.Delete("/:id", domainH.Delete)
	dm.Post("/:id/enable", domainH.Enable)
	dm.Post("/:id/disable", domainH.Disable)

	serviceH := service.NewHandler(service.NewRepository(d.Pool))
	sv := admin.Group("/services")
	sv.Get("/", serviceH.List)
	sv.Post("/", serviceH.Create)
	sv.Get("/:id", serviceH.Get)
	sv.Patch("/:id", serviceH.Update)
	sv.Delete("/:id", serviceH.Delete)
	sv.Post("/:id/enable", serviceH.Enable)
	sv.Post("/:id/disable", serviceH.Disable)

	instanceH := instance.NewHandler(instance.NewRepository(d.Pool))
	ins := admin.Group("/instances")
	ins.Get("/", instanceH.List)
	ins.Post("/register", instanceH.Register)
	ins.Get("/:id", instanceH.Get)
	ins.Patch("/:id", instanceH.Update)
	ins.Delete("/:id", instanceH.Delete)
	ins.Post("/:id/enable", instanceH.Enable)
	ins.Post("/:id/disable", instanceH.Disable)
	ins.Post("/:id/drain", instanceH.Drain)
	ins.Post("/:id/undrain", instanceH.Undrain)
	ins.Post("/:id/health", instanceH.Health)

	// 路由缓存查看/重建。
	if d.RouteCache != nil {
		rc := &routeCacheHandler{cache: d.RouteCache}
		admin.Get("/routes", rc.ListRoutes)
		admin.Get("/cache/status", rc.Status)
		admin.Get("/cache/stats", rc.Stats)
		admin.Post("/cache/rebuild", rc.Rebuild)
	}

	return app
}
