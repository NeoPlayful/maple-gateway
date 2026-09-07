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

	"github.com/NeoPlayful/maple-gateway/server/internal/api"
	"github.com/NeoPlayful/maple-gateway/server/internal/cache"
	"github.com/NeoPlayful/maple-gateway/server/internal/config"
	"github.com/NeoPlayful/maple-gateway/server/internal/domain"
	"github.com/NeoPlayful/maple-gateway/server/internal/gateway"
	"github.com/NeoPlayful/maple-gateway/server/internal/health"
	"github.com/NeoPlayful/maple-gateway/server/internal/instance"
	"github.com/NeoPlayful/maple-gateway/server/internal/proxy"
	"github.com/NeoPlayful/maple-gateway/server/internal/router"
	"github.com/NeoPlayful/maple-gateway/server/internal/service"
	"github.com/NeoPlayful/maple-gateway/server/internal/tenant"
	"github.com/NeoPlayful/maple-gateway/server/pkg"

	"github.com/gofiber/fiber/v3"
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

	// -migrate：执行迁移 + seed 默认管理员后退出。
	if migrate {
		if cfg.Database.URL == "" {
			return fmt.Errorf("migrate requires database.url")
		}
		return runMigration(ctx, cfg.Database.URL)
	}

	// resolver 选择：优先 DB 动态路由（S4），无 DB / 失败时回退静态（S2）。
	resolver, routeCache, db, err := buildResolver(ctx, cfg, routesPath, logger)
	if err != nil {
		return err
	}
	if db != nil {
		defer db.Close()
	}
	if routeCache != nil {
		// 周期重建：DB 里 Tenant/Domain/Service/Instance 变更 5s 内自动生效，无需重启。
		routeCache.AutoRebuild(ctx, 5*time.Second, logger)
	}
	// 主动健康检查：探测异常实例并更新 DB health，AutoRebuild 随之摘除/恢复。
	if db != nil {
		hc := health.NewChecker(health.NewInstanceRepo(instance.NewRepository(db.Pool)), health.Config{
			Interval:         cfg.Health.Interval,
			Timeout:          cfg.Health.Timeout,
			FailureThreshold: cfg.Health.FailureThreshold,
			SuccessThreshold: cfg.Health.SuccessThreshold,
			GracePeriod:      cfg.Health.GracePeriod,
			Path:             "/health",
		}, logger)
		go hc.Run(ctx)
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

	// 组装并启动数据平面。
	dp := gateway.NewDataPlane(gateway.DataPlaneConfig{
		Address:           cfg.Gateway.HTTP.Address,
		Resolver:          resolver,
		Transport:         transport,
		ReadHeaderTimeout: cfg.Proxy.ReadHeaderTimeout,
		IdleTimeout:       cfg.Proxy.IdleTimeout,
		MaxHeaderBytes:    cfg.Proxy.MaxHeaderBytes,
		Logger:            logger,
	})
	dpErrCh := dp.Start()

	// Management API（数据平面与控制面分离）。
	var mgmtApp *fiber.App
	if db != nil {
		mgmtApp = api.New(api.Deps{Pool: db.Pool, RouteCache: routeCache})
	} else {
		mgmtApp = api.New(api.Deps{Pool: nil})
	}
	go func() {
		logger.Info("management api listening", zap.String("addr", cfg.Management.Address))
		if err := mgmtApp.Listen(cfg.Management.Address); err != nil {
			logger.Error("management api listen failed", zap.String("err", err.Error()))
		}
	}()

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
	if err := seedDefaultAdmin(appCtx, db.Pool); err != nil {
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
// *pkg.DB 由调用方负责 Close（为 nil 表示未接入 DB）。
func buildResolver(ctx context.Context, cfg *config.Config, routesPath string,
	logger *zap.Logger) (router.Resolver, *cache.Cache, *pkg.DB, error) {

	// 兜底静态 resolver（无 DB 或失败时）。
	staticRes := router.FromMap(nil)
	if routesPath != "" {
		raw, err := os.ReadFile(routesPath)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("read routes file: %w", err)
		}
		staticRes, err = router.NewStaticResolver(raw)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("parse routes file: %w", err)
		}
	}

	if cfg.Database.URL == "" {
		return staticRes, nil, nil, nil
	}

	appCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	db, err := pkg.NewDB(appCtx, cfg.Database.URL)
	if err != nil {
		logger.Warn("database unavailable, falling back to static routes",
			zap.String("err", err.Error()))
		return staticRes, nil, nil, nil
	}

	rc := cache.New(
		tenant.NewRepository(db.Pool),
		domain.NewRepository(db.Pool),
		service.NewRepository(db.Pool),
		instance.NewRepository(db.Pool),
	)
	if err := rc.Rebuild(ctx); err != nil {
		logger.Warn("route cache rebuild failed, falling back to static routes",
			zap.String("err", err.Error()))
		db.Close()
		return staticRes, nil, nil, nil
	}
	logger.Info("route cache loaded", zap.Int("routes", len(rc.Entries())))
	return cache.NewResolver(rc), rc, db, nil
}
