// Package api 组装 Management API 的全部路由。
package api

import (
	"context"
	"os"
	"path/filepath"
	"time"

	"github.com/NeoPlayful/maple-gateway/server/ent"
	"github.com/NeoPlayful/maple-gateway/server/internal/auth"
	"github.com/NeoPlayful/maple-gateway/server/internal/bluegreen"
	"github.com/NeoPlayful/maple-gateway/server/internal/cache"
	"github.com/NeoPlayful/maple-gateway/server/internal/canary"
	"github.com/NeoPlayful/maple-gateway/server/internal/certificate"
	"github.com/NeoPlayful/maple-gateway/server/internal/cmclient"
	"github.com/NeoPlayful/maple-gateway/server/internal/dashboard"
	"github.com/NeoPlayful/maple-gateway/server/internal/deployment"
	"github.com/NeoPlayful/maple-gateway/server/internal/discovery"
	"github.com/NeoPlayful/maple-gateway/server/internal/domain"
	"github.com/NeoPlayful/maple-gateway/server/internal/ha"
	"github.com/NeoPlayful/maple-gateway/server/internal/instance"
	"github.com/NeoPlayful/maple-gateway/server/internal/logs"
	"github.com/NeoPlayful/maple-gateway/server/internal/metrics"
	"github.com/NeoPlayful/maple-gateway/server/internal/node"
	"github.com/NeoPlayful/maple-gateway/server/internal/ratelimit"
	"github.com/NeoPlayful/maple-gateway/server/internal/rbac"
	"github.com/NeoPlayful/maple-gateway/server/internal/service"
	"github.com/NeoPlayful/maple-gateway/server/internal/settings"
	"github.com/NeoPlayful/maple-gateway/server/internal/system"
	"github.com/NeoPlayful/maple-gateway/server/internal/tenant"
	"github.com/NeoPlayful/maple-gateway/server/internal/traffic"
	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/requestid"
	"github.com/gofiber/fiber/v3/middleware/static"
)

// Deps 是 Management API 所需依赖。
type Deps struct {
	Ent          *ent.Client                 // nil 表示未接入 DB（禁用 admin 与业务接口）
	ReadyDB      func(context.Context) error // DB 就绪探针；nil 表示无 DB（health/ready 报 not-ready）
	RouteCache   *cache.Cache                // 可空；用于 route/cache 查看与手动重建
	Metrics      *metrics.Registry           // 可空；提供 /metrics 导出
	AccessLog    *logs.AccessLog             // 可空；提供访问日志查询
	ErrLog       *logs.ErrLog                // 可空；提供错误日志查询
	Settings     *settings.Repository        // 可空；提供动态 Settings 读写
	Series       dashboard.SeriesReader      // 可空；提供 Dashboard 趋势时序数据
	HA           *ha.Handler                 // 可空；提供 Gateway 自身实例（HA）查看
	Certificates *certificate.Handler        // 可空；提供 Direct TLS 证书管理（需 MAPLE_CERT_ENC_KEY）
	CMClient     *cmclient.Client            // 可空；接入 CM 后由 Leader 下发部署意图
	IsLeader     func() bool                 // 可空；下发前判定本进程是否 Leader（nil 视为单实例）
	UIDir        string                      // 可空；管理后台前端产物目录（dist），空则不托管 UI
}

// New 构造 Fiber app 并注册全部 Management API 路由。
func New(d Deps) *fiber.App {
	app := fiber.New(fiber.Config{
		AppName:     "maple-gateway-mgmt",
		BodyLimit:   4 * 1024 * 1024,
		Concurrency: 1024 * 10,
	})

	// 管理面请求 ID：客户端已带则透传，否则生成（与数据平面同头名，日志/trace 可关联）。
	app.Use(requestid.New(requestid.Config{Header: "X-Request-Id"}))

	// 系统接口（无需认证）。
	sys := system.NewHandler(d.ReadyDB)
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

	if d.Ent == nil {
		return app
	}

	// 认证。
	authSvc := auth.NewService(d.Ent, auth.Secret(), 24*time.Hour)
	authH := auth.NewHandler(authSvc)
	app.Post("/api/auth/login", authH.Login)
	app.Post("/api/auth/refresh", authH.Refresh)

	// 需要登录的管理面路由（含审计 + RBAC）。
	admin := app.Group("/api/admin",
		auth.Middleware(authSvc),
		rbac.Middleware(newDenyAuditer(d.Ent)),
		auditMiddleware(d.Ent),
	)
	admin.Get("/system/info", sys.Info)

	// Auth me/logout/change-password 也放在认证组内。
	admin.Get("/auth/me", authH.Me)
	admin.Post("/auth/logout", authH.Logout)
	admin.Post("/auth/change-password", authH.ChangePassword)

	// 平台用户账号管理（super_admin 专属，写操作经 RBAC 拦截为仅超管）。
	usersH := &userHandler{ent: d.Ent}
	us := admin.Group("/users")
	us.Get("/", usersH.List)
	us.Post("/", usersH.Create)
	us.Patch("/:id/role", usersH.SetRole)
	us.Patch("/:id/status", usersH.ToggleStatus)

	// Internal API：Container Manager / Node Agent 状态上报，独立 MAPLE_INTERNAL_TOKEN 认证。
	// 规范路径定稿 /api/internal/*；/api/internal/discovery/* 作为旧路径保留一个版本过渡（deprecated）。
	disc := discovery.NewHandler(node.NewRepository(d.Ent), instance.NewRepository(d.Ent))
	registerInternal := func(g fiber.Router) {
		g.Post("/nodes/register", disc.RegisterNode)
		g.Patch("/nodes/:id", disc.UpdateNode)
		g.Delete("/nodes/:id", disc.DeleteNode)
		g.Post("/nodes/:id/heartbeat", disc.HeartbeatNode)
		g.Post("/instances/register", disc.RegisterInstance)
		g.Patch("/instances/:id", disc.UpdateInstance)
		g.Delete("/instances/:id", disc.DeleteInstance)
		g.Post("/instances/:id/heartbeat", disc.HeartbeatInstance)
		g.Post("/instances/:id/health", disc.ReportHealth)
		g.Post("/instances/:id/drain", disc.DrainInstance)
		g.Post("/sync", disc.Sync)
	}
	registerInternal(app.Group("/api/internal", discovery.Middleware()))
	registerInternal(app.Group("/api/internal/discovery", discovery.Middleware()))

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

	instanceH := instance.NewHandler(instance.NewRepository(d.Ent))
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

	// Gateway 自身实例（多实例 HA）查看。
	if d.HA != nil {
		hag := admin.Group("/ha")
		hag.Get("/instances", d.HA.List)
		hag.Get("/instances/:id", d.HA.Get)
		hag.Get("/leader", d.HA.Leader)
	}

	deployH := deployment.NewHandler(deployment.NewRepository(d.Ent))
	if d.CMClient != nil && d.CMClient.Enabled() {
		deployH = deployH.WithPusher(d.CMClient, d.IsLeader)
	}
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
	canaryH := canary.NewHandler(canary.NewService(canary.NewRepository(d.Ent)))
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

	// 动态 Settings（含版本历史与回滚）。
	if d.Settings != nil {
		setH := settings.NewHandler(d.Settings)
		admin.Get("/settings", setH.Get)
		admin.Patch("/settings/:section", setH.Update)
		admin.Get("/settings/:section/history", setH.History)
		admin.Patch("/settings/:section/rollback", setH.Rollback)
	}

	// Blue/Green 双版本切换。
	bgH := bluegreen.NewHandler(bluegreen.NewService(bluegreen.NewRepository(d.Ent)))
	bg := admin.Group("/blue-green")
	bg.Get("/", bgH.List)
	bg.Post("/", bgH.Create)
	bg.Get("/:id", bgH.Get)
	bg.Delete("/:id", bgH.Delete)
	bg.Post("/:id/switch", bgH.Switch)
	bg.Post("/:id/rollback", bgH.Rollback)

	// Direct TLS 证书管理（可选：需 MAPLE_CERT_ENC_KEY 才能构造）。
	if d.Certificates != nil {
		ch := d.Certificates
		cert := admin.Group("/certificates")
		cert.Get("/", ch.List)
		cert.Get("/count", ch.Count)
		// 进度查询须在 "/:id" 之前注册，避免 "/operations" 被当作 id 匹配。
		cert.Get("/operations", ch.OperationByHostname)
		cert.Get("/operations/:id", ch.Operation)
		cert.Get("/:id", ch.Get)
		cert.Patch("/:id", ch.Update)
		cert.Delete("/:id", ch.Delete)
		cert.Get("/:id/status", ch.Status)
		cert.Post("/:id/reload", ch.Reload)
		cert.Post("/:id/renew", ch.Renew)
		cert.Post("/issue", ch.Issue)
		cert.Post("/", ch.Upload)
		// 需求路径：POST /api/admin/domains/:id/certificate（域名维度上传）。
		admin.Post("/domains/:id/certificate", ch.Upload)
		// 域名维度签发（Managed/ACME）。
		admin.Post("/domains/:id/certificate/issue", ch.Issue)
		// 域名维度签发进度查询。
		admin.Get("/domains/:id/certificate/progress", ch.DomainProgress)
	}

	// 路由缓存查看/重建。
	if d.RouteCache != nil {
		rc := &routeCacheHandler{cache: d.RouteCache}
		admin.Get("/routes", rc.ListRoutes)
		admin.Get("/cache/status", rc.Status)
		admin.Get("/cache/stats", rc.Stats)
		admin.Post("/cache/rebuild", rc.Rebuild)
	}

	// Dashboard 聚合：总览 + 趋势/细分。
	dash := dashboard.NewHandler(d.Ent, func() (uint64, uint64) {
		if d.RouteCache == nil {
			return 0, 0
		}
		return d.RouteCache.Stats()
	}, d.Series)
	admin.Get("/dashboard/overview", dash.Overview)
	admin.Get("/dashboard/traffic", dash.Traffic)
	admin.Get("/dashboard/errors", dash.Errors)
	admin.Get("/dashboard/latency", dash.Latency)
	admin.Get("/dashboard/instances", dash.Instances)
	admin.Get("/dashboard/nodes", dash.Nodes)
	admin.Get("/dashboard/canary", dash.Canary)

	if d.UIDir != "" {
		mountUI(app, d.UIDir)
	}

	return app
}

// mountUI 把前端构建产物（dist）托管到管理端口：
//   - /admin 与 /admin/* 命中真实文件则返回，未命中（SPA 深层路由）回退 index.html；
//   - /login 直接返回 index.html（独立登录页入口，不进入 /admin 守卫层）。
//
// dist 目录或 index.html 缺失时静默跳过，保持纯 API 模式。
func mountUI(app *fiber.App, uiDir string) {
	indexPath := filepath.Join(uiDir, "index.html")
	if _, err := os.Stat(indexPath); err != nil {
		return
	}
	indexFS := func(c fiber.Ctx) error {
		return c.SendFile(indexPath)
	}
	// root 为真实目录：static 中间件内部 sanitizePath 会拦截 ../、反斜杠、盘符穿越。
	app.Use("/admin", static.New(uiDir, static.Config{NotFoundHandler: indexFS}))
	app.Use("/login", static.New(uiDir, static.Config{NotFoundHandler: indexFS}))
}
