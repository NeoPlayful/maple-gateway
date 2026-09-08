// Package api 组装 Management API 的全部路由。
package api

import (
	"time"

	"github.com/NeoPlayful/maple-gateway/server/internal/auth"
	"github.com/NeoPlayful/maple-gateway/server/internal/bluegreen"
	"github.com/NeoPlayful/maple-gateway/server/internal/cache"
	"github.com/NeoPlayful/maple-gateway/server/internal/canary"
	"github.com/NeoPlayful/maple-gateway/server/internal/dashboard"
	"github.com/NeoPlayful/maple-gateway/server/internal/deployment"
	"github.com/NeoPlayful/maple-gateway/server/internal/discovery"
	"github.com/NeoPlayful/maple-gateway/server/internal/domain"
	"github.com/NeoPlayful/maple-gateway/server/internal/instance"
	"github.com/NeoPlayful/maple-gateway/server/internal/logs"
	"github.com/NeoPlayful/maple-gateway/server/internal/metrics"
	"github.com/NeoPlayful/maple-gateway/server/internal/node"
	"github.com/NeoPlayful/maple-gateway/server/internal/ratelimit"
	"github.com/NeoPlayful/maple-gateway/server/internal/service"
	"github.com/NeoPlayful/maple-gateway/server/internal/settings"
	"github.com/NeoPlayful/maple-gateway/server/internal/system"
	"github.com/NeoPlayful/maple-gateway/server/internal/tenant"
	"github.com/NeoPlayful/maple-gateway/server/internal/traffic"
	"github.com/NeoPlayful/maple-gateway/server/ent"
	"github.com/gofiber/fiber/v3"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Deps 是 Management API 所需依赖。
type Deps struct {
	Pool       *pgxpool.Pool        // nil 表示未接入 DB（禁用 admin 与业务接口）
	Ent        *ent.Client          // 已迁移到 Ent 的模块使用；与 Pool 指向同一库
	RouteCache *cache.Cache         // 可空；用于 route/cache 查看与手动重建
	Metrics    *metrics.Registry    // 可空；提供 /metrics 导出
	AccessLog  *logs.AccessLog      // 可空；提供访问日志查询
	ErrLog     *logs.ErrLog         // 可空；提供错误日志查询
	Settings   *settings.Repository // 可空；提供动态 Settings 读写
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
	if d.Metrics != nil {
		reg := d.Metrics
		app.Get("/metrics", func(c fiber.Ctx) error {
			c.Type("text/plain; version=0.0.4")
			return c.SendString(reg.RenderText())
		})
	}

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

	// Internal API：Container Manager / Node Agent 状态上报，独立 MAPLE_INTERNAL_TOKEN 认证。
	// instance 已迁移 Ent；node 仍走 pgxpool。
	disc := discovery.NewHandler(node.NewRepository(d.Ent), instance.NewRepository(d.Ent, d.Pool))
	internal := app.Group("/api/internal/discovery", discovery.Middleware())
	internal.Post("/nodes/register", disc.RegisterNode)
	internal.Post("/nodes/:id/heartbeat", disc.HeartbeatNode)
	internal.Post("/instances/register", disc.RegisterInstance)
	internal.Patch("/instances/:id", disc.UpdateInstance)
	internal.Delete("/instances/:id", disc.DeleteInstance)
	internal.Post("/instances/:id/health", disc.ReportHealth)

	// 业务模块 CRUD。
	tenantH := tenant.NewHandler(tenant.NewRepository(d.Ent))
	t := admin.Group("/tenants")
	t.Get("/", tenantH.List)
	t.Post("/", tenantH.Create)
	t.Get("/:id", tenantH.Get)
	t.Patch("/:id", tenantH.Update)
	t.Delete("/:id", tenantH.Delete)
	t.Post("/:id/enable", tenantH.Enable)
	t.Post("/:id/disable", tenantH.Disable)
	t.Post("/:id/suspend", tenantH.Suspend)

	domainH := domain.NewHandler(domain.NewRepository(d.Ent))
	dm := admin.Group("/domains")
	dm.Get("/", domainH.List)
	dm.Post("/", domainH.Create)
	dm.Get("/:id", domainH.Get)
	dm.Patch("/:id", domainH.Update)
	dm.Delete("/:id", domainH.Delete)
	dm.Post("/:id/enable", domainH.Enable)
	dm.Post("/:id/disable", domainH.Disable)

	serviceH := service.NewHandler(service.NewRepository(d.Ent))
	sv := admin.Group("/services")
	sv.Get("/", serviceH.List)
	sv.Post("/", serviceH.Create)
	sv.Get("/:id", serviceH.Get)
	sv.Patch("/:id", serviceH.Update)
	sv.Delete("/:id", serviceH.Delete)
	sv.Post("/:id/enable", serviceH.Enable)
	sv.Post("/:id/disable", serviceH.Disable)

	instanceH := instance.NewHandler(instance.NewRepository(d.Ent, d.Pool))
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
	ins.Post("/:id/mount", instanceH.Mount)
	ins.Post("/:id/health", instanceH.Health)

	nodeH := node.NewHandler(node.NewRepository(d.Ent))
	nd := admin.Group("/nodes")
	nd.Get("/", nodeH.List)
	nd.Post("/", nodeH.Create)
	nd.Get("/:id", nodeH.Get)
	nd.Patch("/:id", nodeH.Update)
	nd.Delete("/:id", nodeH.Delete)
	nd.Post("/:id/enable", nodeH.Enable)
	nd.Post("/:id/disable", nodeH.Disable)
	nd.Post("/:id/maintenance", nodeH.Maintenance)
	nd.Post("/:id/heartbeat", nodeH.Heartbeat)

	deployH := deployment.NewHandler(deployment.NewRepository(d.Ent))
	dpl := admin.Group("/deployments")
	dpl.Get("/", deployH.ListDeployments)
	dpl.Post("/", deployH.CreateDeployment)
	dpl.Get("/:id", deployH.GetDeployment)
	dpl.Patch("/:id", deployH.UpdateDeployment)
	dpl.Delete("/:id", deployH.DeleteDeployment)
	dpl.Post("/:id/pause", deployH.PauseDeployment)
	dpl.Post("/:id/resume", deployH.ResumeDeployment)
	dpl.Post("/:id/stop", deployH.StopDeployment)
	dpl.Get("/:id/versions", deployH.ListVersions)
	dpl.Post("/:id/versions", deployH.CreateVersion)

	ver := admin.Group("/versions")
	ver.Get("/:id", deployH.GetVersion)
	ver.Patch("/:id", deployH.UpdateVersion)
	ver.Delete("/:id", deployH.DeleteVersion)
	ver.Post("/:id/default", deployH.SetDefaultVersion)

	trafficH := traffic.NewHandler(traffic.NewRepository(d.Ent))
	tf := admin.Group("/traffic")
	tf.Get("/", trafficH.List)
	tf.Post("/", trafficH.Create)
	tf.Get("/:id", trafficH.Get)
	tf.Patch("/:id", trafficH.Update)
	tf.Delete("/:id", trafficH.Delete)
	tf.Post("/:id/enable", trafficH.Enable)
	tf.Post("/:id/disable", trafficH.Disable)

	// Canary 发布控制。
	canaryH := canary.NewHandler(canary.NewService(canary.NewRepository(d.Ent, d.Pool)))
	cn := admin.Group("/canary")
	cn.Get("/", canaryH.List)
	cn.Post("/", canaryH.Create)
	cn.Get("/:id", canaryH.Get)
	cn.Patch("/:id", canaryH.Update)
	cn.Delete("/:id", canaryH.Delete)
	cn.Post("/:id/start", canaryH.Start)
	cn.Post("/:id/pause", canaryH.Pause)
	cn.Post("/:id/resume", canaryH.Resume)
	cn.Post("/:id/weight", canaryH.SetWeight)
	cn.Post("/:id/promote", canaryH.Promote)
	cn.Post("/:id/rollback", canaryH.Rollback)

	// 限流规则。
	rlH := ratelimit.NewHandler(ratelimit.NewRepository(d.Ent))
	rl := admin.Group("/rate-limits")
	rl.Get("/", rlH.List)
	rl.Post("/", rlH.Create)
	rl.Get("/:id", rlH.Get)
	rl.Patch("/:id", rlH.Update)
	rl.Delete("/:id", rlH.Delete)
	rl.Post("/:id/enable", rlH.Enable)
	rl.Post("/:id/disable", rlH.Disable)

	// 访问 / 错误日志查询。
	if d.AccessLog != nil || d.ErrLog != nil {
		logH := logs.NewHandler(d.AccessLog, d.ErrLog)
		admin.Get("/logs/access", logH.Access)
		admin.Get("/logs/error", logH.Error)
	}

	// 审计日志查询（DB 落库）。
	audH := &auditLogsHandler{ent: d.Ent}
	admin.Get("/logs/audit", audH.List)

	// 动态 Settings。
	if d.Settings != nil {
		setH := settings.NewHandler(d.Settings)
		admin.Get("/settings", setH.Get)
		admin.Patch("/settings/:section", setH.Update)
	}

	// Blue/Green 双版本切换。
	bgH := bluegreen.NewHandler(bluegreen.NewService(bluegreen.NewRepository(d.Ent, d.Pool)))
	bg := admin.Group("/blue-green")
	bg.Get("/", bgH.List)
	bg.Post("/", bgH.Create)
	bg.Get("/:id", bgH.Get)
	bg.Delete("/:id", bgH.Delete)
	bg.Post("/:id/switch", bgH.Switch)
	bg.Post("/:id/rollback", bgH.Rollback)

	// 路由缓存查看/重建。
	if d.RouteCache != nil {
		rc := &routeCacheHandler{cache: d.RouteCache}
		admin.Get("/routes", rc.ListRoutes)
		admin.Get("/cache/status", rc.Status)
		admin.Get("/cache/stats", rc.Stats)
		admin.Post("/cache/rebuild", rc.Rebuild)
	}

	// Dashboard 聚合。
	dash := dashboard.NewHandler(d.Pool, func() (uint64, uint64) {
		if d.RouteCache == nil {
			return 0, 0
		}
		return d.RouteCache.Stats()
	})
	admin.Get("/dashboard/overview", dash.Overview)

	return app
}
