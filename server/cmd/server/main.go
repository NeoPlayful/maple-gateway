// Maple Gateway 服务入口。
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/NeoPlayful/maple-gateway/server/ent"
	"github.com/NeoPlayful/maple-gateway/server/internal/api"
	"github.com/NeoPlayful/maple-gateway/server/internal/cache"
	"github.com/NeoPlayful/maple-gateway/server/internal/config"
	"github.com/NeoPlayful/maple-gateway/server/internal/deployment"
	"github.com/NeoPlayful/maple-gateway/server/internal/domain"
	"github.com/NeoPlayful/maple-gateway/server/internal/gateway"
	"github.com/NeoPlayful/maple-gateway/server/internal/health"
	"github.com/NeoPlayful/maple-gateway/server/internal/instance"
	"github.com/NeoPlayful/maple-gateway/server/internal/logs"
	"github.com/NeoPlayful/maple-gateway/server/internal/metrics"
	"github.com/NeoPlayful/maple-gateway/server/internal/node"
	"github.com/NeoPlayful/maple-gateway/server/internal/proxy"
	"github.com/NeoPlayful/maple-gateway/server/internal/ratelimit"
	"github.com/NeoPlayful/maple-gateway/server/internal/router"
	"github.com/NeoPlayful/maple-gateway/server/internal/service"
	"github.com/NeoPlayful/maple-gateway/server/internal/settings"
	"github.com/NeoPlayful/maple-gateway/server/internal/tenant"
	"github.com/NeoPlayful/maple-gateway/server/internal/traffic"
	"github.com/NeoPlayful/maple-gateway/server/pkg"

	"github.com/gofiber/fiber/v3"
	"github.com/joho/godotenv"
	"go.uber.org/zap"
)

func main() {
	var (
		configPath  = flag.String("config", "", "config file path (YAML)")
		routesPath  = flag.String("routes", "", "static routes file path (YAML), S2 阶段用")
		migrate     = flag.Bool("migrate", false, "run database migrations then seed admin, then exit")
		showExample = flag.Bool("print-config", false, "print effective config and exit")
	)
	flag.Parse()

	// 从 cwd 加载 .env（不存在则忽略）。已注入的环境变量优先于 .env。
	_ = godotenv.Load()

	if err := run(*configPath, *routesPath, *migrate, *showExample); err != nil {
		log.Fatalf("maple-gateway: %v", err)
	}
}

func run(configPath, routesPath string, migrate, showExample bool) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	cfg, err := config.Load(configPath)
	if err != nil {
		return err
	}
	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("config validation: %w", err)
	}

	if err := pkg.InitLogger(cfg.Logging.Level, cfg.Logging.Pretty); err != nil {
		return fmt.Errorf("init logger: %w", err)
	}
	defer pkg.SyncLogger()

	if showExample {
		fmt.Println("config loaded ok")
	}

	logger := pkg.Log()

	// Redis（可选组件）：enabled && url 配置时才建立连接并探测；
	// 连接失败只告警降级，不阻断启动（组件可按需接入）。
	var redisClient *pkg.Redis
	if cfg.Redis.Enabled && cfg.Redis.URL != "" {
		redisClient, err = pkg.NewRedis(ctx, cfg.Redis.URL)
		if err != nil {
			logger.Warn("redis connect failed, running without redis",
				zap.String("err", err.Error()))
		} else {
			logger.Info("redis connected",
				zap.String("url", cfg.Redis.URL))
			defer redisClient.Close()
		}
	}

	// -migrate：执行迁移 + seed 默认管理员后退出。
	if migrate {
		if cfg.Database.URL == "" {
			return fmt.Errorf("migrate requires database.url")
		}
		return runMigration(ctx, cfg.Database.URL)
	}

	// resolver 选择：优先 DB 动态路由（S4），无 DB / 失败时回退静态（S2）。
	metricReg := metrics.NewRegistry()
	resolver, routeCache, db, entClient, err := buildResolver(ctx, cfg, routesPath, logger, metricReg)
	if err != nil {
		return err
	}
	// 数据平面趋势时间桶：周期快照差分，供 Dashboard 趋势接口。15s/窗口，保留 30 分钟。
	series := metrics.NewTimeSeries(metricReg, metrics.SeriesConfig{Window: 15 * time.Second, Buckets: 120})
	go series.Run(ctx)
	if db != nil {
		defer db.Close()
	}
	if routeCache != nil {
		// 周期重建：DB 里 Tenant/Domain/Service/Instance 变更 5s 内自动生效，无需重启。
		routeCache.AutoRebuild(ctx, 5*time.Second, logger)
	}
	// 主动健康检查：探测异常实例并更新 DB health，AutoRebuild 随之摘除/恢复。
	if db != nil {
		hc := health.NewChecker(health.NewInstanceRepo(instance.NewRepository(entClient)), health.Config{
			Interval:         cfg.Health.Interval,
			Timeout:          cfg.Health.Timeout,
			FailureThreshold: cfg.Health.FailureThreshold,
			SuccessThreshold: cfg.Health.SuccessThreshold,
			GracePeriod:      cfg.Health.GracePeriod,
			Path:             "/health",
		}, logger).WithMetrics(metricReg)
		go hc.Run(ctx)
	}
	// Node 心跳看护：超时未心跳的节点置 offline，其上实例随 AutoRebuild 摘除。
	if db != nil {
		node.Watchdog(ctx, node.NewRepository(entClient), node.WatchdogConfig{
			Interval: 10 * time.Second,
			Timeout:  30 * time.Second,
			Logger:   logger,
		})
	}

	logger.Info("maple-gateway starting",
		zap.String("http_addr", cfg.Gateway.HTTP.Address),
		zap.String("management_addr", cfg.Management.Address),
		zap.Bool("route_cache_enabled", routeCache != nil),
		zap.Bool("static_routes", routesPath != ""),
	)

	// 根据配置构造 upstream transport，使超时配置真实生效。
	transport := proxy.NewTransport(proxy.TransportConfig{
		ResponseHeaderTimeout: cfg.Proxy.ResponseHeaderTimeout,
		IdleTimeout:           cfg.Proxy.IdleTimeout,
		MaxIdleConns:          200,
		MaxIdleConnsPerHost:   20,
	})

	// 组装并启动数据平面（HTTP 与 HTTPS 可并行；证书齐全才启用 HTTPS）。
	httpAddr := ""
	if cfg.Gateway.HTTP.Enabled {
		httpAddr = cfg.Gateway.HTTP.Address
	}
	httpsAddr, certFile, keyFile := "", "", ""
	if cfg.Gateway.HTTPS.Enabled && cfg.Gateway.HTTPS.Cert != "" && cfg.Gateway.HTTPS.Key != "" {
		httpsAddr = cfg.Gateway.HTTPS.Address
		certFile = cfg.Gateway.HTTPS.Cert
		keyFile = cfg.Gateway.HTTPS.Key
	}
	accessLog := logs.NewAccessLog(5000)
	errLog := logs.NewErrLog(2000)
	dp := gateway.NewDataPlane(gateway.DataPlaneConfig{
		Address:           httpAddr,
		HTTPSAddress:      httpsAddr,
		CertFile:          certFile,
		KeyFile:           keyFile,
		Resolver:          resolver,
		Transport:         transport,
		ReadHeaderTimeout: cfg.Proxy.ReadHeaderTimeout,
		IdleTimeout:       cfg.Proxy.IdleTimeout,
		MaxHeaderBytes:    cfg.Proxy.MaxHeaderBytes,
		Logger:            logger,
		Metrics:           metricReg,
		AccessLog:         accessLog,
		ErrLog:            errLog,
	})
	dpErrCh := dp.Start()

	// Management API（数据平面与控制面分离）。
	uiDir := resolveUIDir(cfg.Management.UIDir)
	if uiDir != "" {
		logger.Info("management ui enabled", zap.String("dir", uiDir))
	}
	var mgmtApp *fiber.App
	if db != nil {
		setRepo := settings.NewRepository(entClient)
		if err := setRepo.Reload(ctx); err != nil {
			logger.Warn("settings reload failed", zap.String("err", err.Error()))
		} else {
			// 启动即补 logging.debug 默认行（幂等），保证设置页日志分区恒有开关可显示。
			if err := settings.EnsureDefaultDebug(ctx, setRepo); err != nil {
				logger.Warn("ensure default settings failed", zap.String("err", err.Error()))
			}
			// 启动即应用已存的 logging.debug 开关。
			settings.SyncLogLevel(setRepo)
		}
		mgmtApp = api.New(api.Deps{Ent: entClient, ReadyDB: db.SQL.PingContext, RouteCache: routeCache, Metrics: metricReg,
			AccessLog: accessLog, ErrLog: errLog, Settings: setRepo, Series: series, UIDir: uiDir})
	} else {
		mgmtApp = api.New(api.Deps{Ent: nil, Metrics: metricReg, AccessLog: accessLog, ErrLog: errLog, Series: series, UIDir: uiDir})
	}
	go func() {
		logger.Info("management api listening", zap.String("addr", cfg.Management.Address))
		if err := mgmtApp.Listen(cfg.Management.Address); err != nil {
			logger.Error("management api listen failed", zap.String("err", err.Error()))
		}
	}()

	// 等 Fiber 打印完启动 banner 后再打进程状态汇总，避免输出顺序错乱。
	time.Sleep(150 * time.Millisecond)
	printStartupSummary(cfg.Management.Address, db, redisClient)

	select {
	case <-ctx.Done():
		logger.Info("shutdown signal received")
	case err := <-dpErrCh:
		if err != nil {
			return err
		}
	}

	// 优雅关闭，等待在途请求（最长 10s）。
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := mgmtApp.Shutdown(); err != nil {
		logger.Warn("management shutdown", zap.String("err", err.Error()))
	}
	if err := dp.Shutdown(shutdownCtx); err != nil {
		return err
	}
	logger.Info("gateway stopped cleanly")
	return nil
}

// 启动汇总的颜色码（ANSI 绿色 INFO 前缀，对齐 Shopirea 打印风格）。
const (
	colorGreen = "\033[32m"
	colorReset = "\033[0m"
)

// printStartupSummary 打印进程启动状态汇总：版本、监听地址、DB/Redis 连接状态。
// 不依赖全局单例，直接读取 main 里已有的局部变量（nil 即未接入/未连接）。
func printStartupSummary(addr string, db *pkg.DB, redis *pkg.Redis) {
	fmt.Println("--------------------------------------------------")
	fmt.Printf("%sINFO%s %-26s %s\n", colorGreen, colorReset, "Version:", pkg.Version)
	fmt.Printf("%sINFO%s %-26s %s\n", colorGreen, colorReset, "Management API:", addr)
	if db != nil {
		fmt.Printf("%sINFO%s %-26s %s\n", colorGreen, colorReset, "Database:", "connected")
	} else {
		fmt.Printf("%sINFO%s %-26s %s\n", colorGreen, colorReset, "Database:", "(not connected)")
	}
	if redis != nil {
		fmt.Printf("%sINFO%s %-26s %s\n", colorGreen, colorReset, "Redis:", "connected")
	} else {
		fmt.Printf("%sINFO%s %-26s %s\n", colorGreen, colorReset, "Redis:", "(not connected)")
	}
	fmt.Println()
}

// resolveUIDir 解析前端产物目录：显式配置优先，否则自动探测仓库 frontend/dist，
// 找不到返回空串（不托管 UI）。
func resolveUIDir(configured string) string {
	if configured != "" {
		return configured
	}
	for _, rel := range []string{"frontend/dist", "../frontend/dist"} {
		if st, err := os.Stat(filepath.Join(rel, "index.html")); err == nil && !st.IsDir() {
			abs, aerr := filepath.Abs(rel)
			if aerr != nil {
				return rel
			}
			return abs
		}
	}
	return ""
}

// runMigration 连接 DB 并执行迁移与 seed。
func runMigration(ctx context.Context, dbURL string) error {
	appCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	db, err := pkg.NewDB(appCtx, dbURL)
	if err != nil {
		return fmt.Errorf("connect database: %w", err)
	}
	defer db.Close()

	// migrations 目录相对可执行文件所在位置解析：优先同目录，再回退仓库根。
	dir := "migrations"
	if _, err := os.Stat(filepath.Join("..", "migrations")); err == nil {
		dir = filepath.Join("..", "migrations")
	}
	mig := pkg.NewMigrator(db.Pool, dir)
	if err := mig.Run(appCtx); err != nil {
		return err
	}
	entClient := pkg.NewEntClient(db)
	if err := seedDefaultAdmin(appCtx, entClient); err != nil {
		return err
	}
	pkg.Log().Info("migrations applied, admin seeded")
	return nil
}

// buildResolver 选择数据平面 resolver：
//   - 配置了 DB 且可连：建立内存路由缓存并全量加载，返回 CacheResolver。
//   - 无 DB / DB 失败：回退静态路由文件（routesPath 为空则空路由表）。
//
// 返回的 *cache.Cache 供 Management API（S5）重建/查看路由表用；
// *pkg.DB 与 *ent.Client 由调用方负责 Close/释放（为 nil 表示未接入 DB）。
// metricReg 注入路由缓存与解析器的指标埋点；可为 nil。
func buildResolver(ctx context.Context, cfg *config.Config, routesPath string,
	logger *zap.Logger, metricReg *metrics.Registry) (router.Resolver, *cache.Cache, *pkg.DB, *ent.Client, error) {

	// 兜底静态 resolver（无 DB 或失败时）。
	staticRes := router.FromMap(nil)
	if routesPath != "" {
		raw, err := os.ReadFile(routesPath)
		if err != nil {
			return nil, nil, nil, nil, fmt.Errorf("read routes file: %w", err)
		}
		staticRes, err = router.NewStaticResolver(raw)
		if err != nil {
			return nil, nil, nil, nil, fmt.Errorf("parse routes file: %w", err)
		}
	}

	if cfg.Database.URL == "" {
		return staticRes, nil, nil, nil, nil
	}

	appCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	db, err := pkg.NewDB(appCtx, cfg.Database.URL)
	if err != nil {
		logger.Warn("database unavailable, falling back to static routes",
			zap.String("err", err.Error()))
		return staticRes, nil, nil, nil, nil
	}
	entClient := pkg.NewEntClient(db)

	nodeRepo := node.NewRepository(entClient)
	rlRepo := ratelimit.NewRepository(entClient)
	rc := cache.NewVersioned(
		tenant.NewRepository(entClient),
		domain.NewRepository(entClient),
		service.NewRepository(entClient),
		instance.NewRepository(entClient),
		cache.NewVersionSource(deployment.NewRepository(entClient), traffic.NewRepository(entClient)),
	).WithNodeFilter(nodeRepo.RoutableMap).
		WithLimits(func(ctx context.Context) ([]ratelimit.RateLimit, error) {
			rows, err := rlRepo.All(ctx)
			if err != nil {
				return nil, err
			}
			out := make([]ratelimit.RateLimit, 0, len(rows))
			for _, rl := range rows {
				out = append(out, *rl)
			}
			return out, nil
		})
	if err := rc.Rebuild(ctx); err != nil {
		logger.Warn("route cache rebuild failed, falling back to static routes",
			zap.String("err", err.Error()))
		db.Close()
		return staticRes, nil, nil, nil, nil
	}
	logger.Info("route cache loaded", zap.Int("routes", len(rc.Entries())))
	if metricReg != nil {
		rc.WithMetrics(metricReg)
	}
	return cache.NewResolver(rc).WithMetrics(metricReg), rc, db, entClient, nil
}
