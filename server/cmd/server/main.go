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
	"github.com/NeoPlayful/maple-gateway/server/internal/certificate"
	"github.com/NeoPlayful/maple-gateway/server/internal/certificate/acme"
	"github.com/NeoPlayful/maple-gateway/server/internal/cmclient"
	"github.com/NeoPlayful/maple-gateway/server/internal/config"
	"github.com/NeoPlayful/maple-gateway/server/internal/deployment"
	"github.com/NeoPlayful/maple-gateway/server/internal/domain"
	"github.com/NeoPlayful/maple-gateway/server/internal/gateway"
	"github.com/NeoPlayful/maple-gateway/server/internal/ha"
	"github.com/NeoPlayful/maple-gateway/server/internal/health"
	"github.com/NeoPlayful/maple-gateway/server/internal/instance"
	"github.com/NeoPlayful/maple-gateway/server/internal/logs"
	"github.com/NeoPlayful/maple-gateway/server/internal/metrics"
	"github.com/NeoPlayful/maple-gateway/server/internal/node"
	"github.com/NeoPlayful/maple-gateway/server/internal/proxy"
	"github.com/NeoPlayful/maple-gateway/server/internal/ratelimit"
	"github.com/NeoPlayful/maple-gateway/server/internal/release"

	"github.com/NeoPlayful/maple-gateway/server/internal/router"
	"github.com/NeoPlayful/maple-gateway/server/internal/service"
	"github.com/NeoPlayful/maple-gateway/server/internal/settings"
	"github.com/NeoPlayful/maple-gateway/server/internal/tenant"
	"github.com/NeoPlayful/maple-gateway/server/internal/tracex"
	"github.com/NeoPlayful/maple-gateway/server/internal/traffic"
	"github.com/NeoPlayful/maple-gateway/server/pkg"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
	"github.com/joho/godotenv"
	"go.uber.org/zap"
)

func main() {
	var (
		configPath  = flag.String("config", "", "config file path (YAML)")
		routesPath  = flag.String("routes", "", "static routes file path (YAML), S2 阶段用")
		migrate     = flag.Bool("migrate", false, "run database migrations, then exit")
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

	// OTel 追踪（可选）：开启时初始化 stdout exporter + 采样；span 记录 request_id 关联日志。
	trc, closeTracer, err := tracex.Setup(tracex.Config{
		Enabled:     cfg.Trace.Enabled,
		SampleRatio: cfg.Trace.SampleRatio,
		ServiceName: "maple-gateway",
	})
	if err != nil {
		return fmt.Errorf("init trace: %w", err)
	}
	defer closeTracer()
	if trc != nil {
		logger.Info("otel tracing enabled",
			zap.Float64("sample_ratio", cfg.Trace.SampleRatio))
	}

	// Redis（可选组件）：enabled && url 配置时才建立连接并探测；
	// 连接失败只告警降级，不阻断启动（组件可按需接入）。
	var redisClient *pkg.Redis
	if cfg.Redis.Enabled && cfg.Redis.URL != "" {
		redisClient, err = pkg.NewRedis(ctx, cfg.Redis.URL, cfg.Redis.Prefix)
		if err != nil {
			logger.Warn("redis connect failed, running without redis",
				zap.String("err", err.Error()))
		} else {
			logger.Info("redis connected",
				zap.String("url", cfg.Redis.URL))
			defer redisClient.Close()
		}
	}

	// -migrate：执行迁移后退出。种子数据填充由独立命令 cmd/seed 负责。
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
	// 分布式限流（可选）：mode=redis 且 Redis 可用时，把路由解析器的限流计数切到
	// Redis（跨实例共享配额）；否则保持默认单机内存。Redis 断连由 RedisLimiter fail-open。
	if cr, ok := resolver.(*cache.CacheResolver); ok && cfg.RateLimit.Mode == "redis" && redisClient != nil {
		if rl := ratelimit.NewRedisLimiter(redisClient); rl != nil {
			cr.WithLimiter(rl)
			logger.Info("rate limit backend: redis (shared across instances)")
		}
	}
	// 数据平面趋势时间桶：周期快照差分，供 Dashboard 趋势接口。15s/窗口，保留 30 分钟。
	series := metrics.NewTimeSeries(metricReg, metrics.SeriesConfig{Window: 15 * time.Second, Buckets: 120})
	go series.Run(ctx)
	if db != nil {
		defer db.Close()
	}
	// 动态运行时配置：settings 仓储须在数据面与健康检查器构造之前加载，
	// 使这些消费方读取 DB 覆盖后的值。优先级：内建默认 < YAML/env < DB。
	// DB 不可用时回退到 cfg（YAML/env），进程仍能正常启动。
	var setRepo *settings.Repository
	var healthChecker *health.Checker
	if db != nil {
		setRepo = settings.NewRepository(entClient)
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
	}
	// runtimeHealth / runtimeProxy 每次调用都从 settings 缓存读当前值：阈值等热更项
	// 在消费方每轮读时即时生效。DB 缺行或不可用时逐键回退 cfg（YAML/env）默认。
	runtimeHealth := func() settings.HealthRuntime {
		def := settings.HealthRuntime{
			Interval:         cfg.Health.Interval,
			Timeout:          cfg.Health.Timeout,
			FailureThreshold: cfg.Health.FailureThreshold,
			SuccessThreshold: cfg.Health.SuccessThreshold,
			GracePeriod:      cfg.Health.GracePeriod,
		}
		if setRepo == nil {
			return def
		}
		return setRepo.Health(def)
	}
	runtimeProxy := func() settings.ProxyRuntime {
		def := settings.ProxyRuntime{
			ReadHeaderTimeout:     cfg.Proxy.ReadHeaderTimeout,
			ReadTimeout:           cfg.Proxy.ReadTimeout,
			ResponseHeaderTimeout: cfg.Proxy.ResponseHeaderTimeout,
			IdleTimeout:           cfg.Proxy.IdleTimeout,
			MaxHeaderBytes:        cfg.Proxy.MaxHeaderBytes,
			MaxBodyBytes:          cfg.Proxy.MaxBodyBytes,
			MaxConnsPerHost:       cfg.Proxy.MaxConnsPerHost,
			MaxIdleConns:          cfg.Proxy.MaxIdleConns,
			MaxIdleConnsPerHost:   cfg.Proxy.MaxIdleConnsPerHost,
			MaxInFlight:           cfg.Proxy.MaxInFlight,
		}
		if setRepo == nil {
			return def
		}
		return setRepo.Proxy(def)
	}
	// runtimeACME 读取 ACME 运行时配置（含目录/邮箱/密钥类型/续期参数），逐键被 DB 覆盖。
	runtimeACME := func() settings.ACMERuntime {
		def := settings.ACMERuntime{
			Enabled:            cfg.ACME.Enabled,
			DirectoryURL:       cfg.ACME.DirectoryURL,
			Email:              cfg.ACME.Email,
			Challenge:          cfg.ACME.Challenge,
			KeyType:            cfg.ACME.KeyType,
			RenewBefore:        cfg.ACME.RenewBefore,
			RenewCheckInterval: cfg.ACME.RenewCheckInterval,
			MaxRenewAttempts:   cfg.ACME.MaxRenewAttempts,
			RateLimitBackoff:   cfg.ACME.RateLimitBackoff,
		}
		if setRepo == nil {
			return def
		}
		return setRepo.ACME(def)
	}
	// renewConfig 由 ACME 运行时配置导出续期引擎参数（Before/MaxAttempts/RateBackoff/BaseBackoff）。
	renewConfig := func() certificate.RenewConfig {
		a := runtimeACME()
		return certificate.RenewConfig{
			Before:      a.RenewBefore,
			MaxAttempts: a.MaxRenewAttempts,
			RateBackoff: a.RateLimitBackoff,
			BaseBackoff: time.Hour,
		}
	}
	if routeCache != nil {
		// 周期重建：DB 里 Tenant/Domain/Service/Instance 变更 5s 内自动生效，无需重启。
		routeCache.AutoRebuild(ctx, 5*time.Second, logger)
	}
	// 主动健康检查：探测异常实例并更新 DB health，AutoRebuild 随之摘除/恢复。
	if db != nil {
		dbHealth := runtimeHealth()
		hc := health.NewChecker(health.NewInstanceRepo(instance.NewRepository(entClient)), health.Config{
			Interval:         dbHealth.Interval,
			Timeout:          dbHealth.Timeout,
			FailureThreshold: dbHealth.FailureThreshold,
			SuccessThreshold: dbHealth.SuccessThreshold,
			GracePeriod:      dbHealth.GracePeriod,
			Path:             "/health",
		}, logger).WithMetrics(metricReg)
		go hc.Run(ctx)
		healthChecker = hc
	}
	// Node 心跳看护：超时未心跳的节点置 offline，其上实例随 AutoRebuild 摘除。
	if db != nil {
		node.Watchdog(ctx, node.NewRepository(entClient), node.WatchdogConfig{
			Interval: 10 * time.Second,
			Timeout:  30 * time.Second,
			Logger:   logger,
		})
	}
	// 自动 Canary：指标驱动自动推进/止损（需 db + 指标时间桶；默认关闭）。
	if db != nil && cfg.CanaryAuto.Enabled {
		relRepo := release.NewRepository(entClient)
		autoRunner := release.NewAuto(release.NewService(relRepo), relRepo,
			release.NewVersionMetricReader(series), logger, release.AutoConfig{
				Enabled:      cfg.CanaryAuto.Enabled,
				Interval:     cfg.CanaryAuto.Interval,
				ErrRateMax:   cfg.CanaryAuto.ErrRateMax,
				ErrLatencyMS: cfg.CanaryAuto.ErrLatencyMS,
				MinRequests:  cfg.CanaryAuto.MinRequests,
			})
		go autoRunner.Run(ctx)
		logger.Info("auto canary enabled")
	}

	// 多实例 HA：本进程在 gateway_instances 注册 + 心跳；启用协调时竞逐 Leader
	// （Redis 锁优先，断连降级 DB lease）。实例 ID 缺省自动生成，进程内保持稳定。
	// 该身份同时写入数据平面访问日志（gateway_instance 字段），故在 HA 之外也需可用。
	instanceID := cfg.HA.InstanceID
	if instanceID == "" {
		instanceID = uuid.NewString()[:8]
	}
	nodeName, _ := os.Hostname()
	var coord *ha.Coordinator
	if db != nil {
		coord = ha.NewCoordinator(ha.NewRepository(entClient), redisClient, logger, ha.Config{
			InstanceID: instanceID,
			Addr:       cfg.Addr(cfg.Management.Port),
			Version:    pkg.Version,
			Enabled:    cfg.HA.Enabled,
			Heartbeat:  cfg.HA.Heartbeat,
			LeaseTTL:   cfg.HA.LeaseTTL,
		})
		go coord.Run(ctx)
	}

	// Gateway → CM 下发通道：配置了 CM 地址才启用；未配置时下发为 no-op，
	// Gateway 行为与未接入 CM 时完全一致。多实例时仅 Leader 下发，避免重复。
	cmClient := cmclient.New(cfg.CM.BaseURL, cfg.CM.Token)
	isLeader := func() bool { return coord == nil || !cfg.HA.Enabled || coord.IsLeader() }
	if cmClient.Enabled() {
		logger.Info("container manager channel enabled", zap.String("cm_base_url", cfg.CM.BaseURL))
	}

	// Phase 5 Direct TLS：证书管理 Service（需 DB + MAPLE_CERT_ENC_KEY）。
	// 密钥未配置或表未迁移时告警禁用，不影响既有 global 模式启动。
	var certSvc *certificate.Service
	var certH *certificate.Handler
	var acmeChallenges *acme.ChallengeStore
	if db == nil {
		logger.Debug("certificate management API disabled: database unavailable " +
			"(certificates endpoints not registered)")
	}
	if db != nil {
		certCache := certificate.NewCache()
		certSvc, err = certificate.NewService(certificate.NewRepository(entClient), certCache, logger,
			certificate.WithDomainSync(domain.NewRepository(entClient)),
			certificate.WithEncKey(cfg.TLS.CertEncKey),
			// 异步签发进度：操作记录落库 + 后台任务根上下文（随进程关闭取消）。
			certificate.WithOperations(certificate.NewOperationRepository(entClient), ctx, 10*time.Minute),
			certificate.WithMetrics(metricReg))
		if err != nil {
			logger.Debug("certificate service disabled (Direct TLS)",
				zap.String("err", err.Error()),
				zap.Bool("cert_enc_key_set", cfg.TLS.CertEncKey != "" || os.Getenv("MAPLE_CERT_ENC_KEY") != ""))
			// DB 里 enabled=true 但进程无加密密钥：私钥加密不可降级，ACME 无法生效。
			// 显式告警而非静默忽略——这是「键可写但无法生效」的唯一情形。
			if runtimeACME().Enabled {
				logger.Warn("acme is enabled in settings but certificate service is unavailable " +
					"(missing tls.cert_enc_key): automatic issuance disabled until a key is configured")
			}
		} else {
			if err := certSvc.ReloadAll(ctx); err != nil {
				logger.Warn("certificate cache initial reload failed",
					zap.String("err", err.Error()))
			} else {
				logger.Info("certificate cache loaded",
					zap.Int("certs", certSvc.Cache().Len()))
			}
			certH = certificate.NewHandler(certSvc)
			logger.Debug("certificate management API mounted",
				zap.Bool("cert_enc_key_set", cfg.TLS.CertEncKey != "" || os.Getenv("MAPLE_CERT_ENC_KEY") != ""))
			// ACME 来源：无条件注册 provider 并建挑战 store（空 store 不命中任何路径）。
			// enabled 由运行期配置决定：关闭时 provider 拒绝签发、续期循环跳过扫描。
			// 这样 enabled 才能在运行期热启停，而无需重启进程重建数据平面接线。
			acmeChallenges = acme.NewChallengeStore()
			acmeRuntime := runtimeACME()
			certSvc.EnableACME(entClient, acmeChallenges, certificate.ACMEConfig{
				Enabled:      acmeRuntime.Enabled,
				DirectoryURL: acmeRuntime.DirectoryURL,
				Email:        acmeRuntime.Email,
				Challenge:    acmeRuntime.Challenge,
				KeyType:      acmeRuntime.KeyType,
			})
		}
	}
	// 证书缓存多实例对账：周期全量重载（对齐 Phase 4"先轮询"决策）。
	// 任实例上传/删除证书后，其余实例在窗口内收敛；本地变更已即时更新缓存，幂等。
	if certSvc != nil {
		go func() {
			ticker := time.NewTicker(5 * time.Second)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					if err := certSvc.ReloadAll(ctx); err != nil {
						logger.Warn("certificate cache reconcile failed",
							zap.String("err", err.Error()))
					}
				}
			}
		}()
		// 清理超时未完成的签发操作（实例重启/进程中断兜底）。
		go func() {
			ticker := time.NewTicker(6 * time.Minute)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					if n, err := certSvc.ExpireStaleOperations(ctx, 15*time.Minute); err != nil {
						logger.Warn("certificate operation cleanup failed",
							zap.String("err", err.Error()))
					} else if n > 0 {
						logger.Info("stale certificate operations expired", zap.Int("count", n))
					}
				}
			}
		}()
		// 证书临期扫描（30 天窗口）：置 expiring/expired 并摘除缓存。
		go func() {
			ticker := time.NewTicker(1 * time.Hour)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					if n, err := certSvc.ScanExpiring(ctx, 30*24*time.Hour); err != nil {
						logger.Warn("certificate expiry scan failed",
							zap.String("err", err.Error()))
					} else if n > 0 {
						logger.Info("certificate expiry scan updated", zap.Int("certs", n))
					}
				}
			}
		}()
		// ACME 自动续期引擎。循环常驻（enabled 关闭时内部跳过扫描），参数与间隔可热更。
		// 多实例下仅 Leader 执行（复用 HA 协调），避免重复向 CA 下单；单实例恒执行。
		go certSvc.RenewLoop(ctx, runtimeACME().RenewCheckInterval, func() bool {
			if !cfg.HA.Enabled {
				return true
			}
			return coord != nil && coord.IsLeader()
		})
		{
			a := runtimeACME()
			logger.Info("acme auto-renew loop started",
				zap.Bool("enabled", a.Enabled),
				zap.Duration("renew_before", a.RenewBefore),
				zap.Duration("interval", a.RenewCheckInterval))
		}
	}

	logger.Info("maple-gateway starting",
		zap.String("http_addr", cfg.Addr(cfg.Gateway.HTTP.Port)),
		zap.String("management_addr", cfg.Addr(cfg.Management.Port)),
		zap.Bool("route_cache_enabled", routeCache != nil),
		zap.Bool("static_routes", routesPath != ""),
	)

	// 根据配置构造 upstream transport，使超时与连接池上限真实生效。
	// transport 字段在构造时冻结（Serve 后不可并发改），故这些项为「重启生效」；
	// 运行时可热更的是 MaxInFlight / MaxBodyBytes（见下 DataPlane）。
	px := runtimeProxy()
	transport := proxy.NewTransport(proxy.TransportConfig{
		ResponseHeaderTimeout: px.ResponseHeaderTimeout,
		IdleTimeout:           px.IdleTimeout,
		MaxConnsPerHost:       px.MaxConnsPerHost,
		MaxIdleConns:          px.MaxIdleConns,
		MaxIdleConnsPerHost:   px.MaxIdleConnsPerHost,
	})

	// 组装并启动数据平面（HTTP 与 HTTPS 可并行；证书齐全才启用 HTTPS）。
	httpAddr := ""
	if cfg.Gateway.HTTP.Enabled {
		httpAddr = cfg.Addr(cfg.Gateway.HTTP.Port)
	}
	httpsAddr, certFile, keyFile := "", "", ""
	if cfg.Gateway.HTTPS.Enabled && cfg.Gateway.HTTPS.Cert != "" && cfg.Gateway.HTTPS.Key != "" {
		httpsAddr = cfg.Addr(cfg.Gateway.HTTPS.Port)
		certFile = cfg.Gateway.HTTPS.Cert
		keyFile = cfg.Gateway.HTTPS.Key
	}
	// Phase 5 Direct TLS：tls.mode=direct 且证书缓存可用时注入 SNI GetCertificate。
	// 证书源缺失（无 DB / 无 MAPLE_CERT_ENC_KEY）时 certSvc==nil：
	// 数据面（server.go direct 分支）会注入恒拒占位——HTTPS 全部拒绝握手，进程不退出（B 方案）。
	tlsMode := gateway.TLSMode(cfg.TLS.Mode)
	if tlsMode == "" {
		tlsMode = gateway.TLSModeDirect
	}
	var sniGetter gateway.TLSCertGetter
	if tlsMode == gateway.TLSModeDirect && certSvc != nil {
		// SNI 未命中在 direct 模式是预期事件（每连接触发一次），降为 Debug；
		// 默认 info 级别下静默，需排查时再开 debug。频繁拒绝请查 Stats() 计数。
		onMiss := func(serverName, clientAddr string, usedFallback bool) {
			logger.Debug("tls certificate cache miss",
				zap.String("sni", serverName),
				zap.String("client_addr", clientAddr),
				zap.Bool("used_fallback", usedFallback))
		}
		g := certificate.NewGetter(certSvc.Cache(), cfg.TLS.FallbackCertEnabled, onMiss)
		sniGetter = g.GetCertificate
		if certH != nil {
			certH.SetGetter(g)
		}
		logger.Info("data plane tls mode direct (dynamic sni)",
			zap.Bool("fallback_cert_enabled", cfg.TLS.FallbackCertEnabled),
			zap.Int("cached_certs", certSvc.Cache().Len()))
	} else if tlsMode == gateway.TLSModeDirect {
		logger.Warn("data plane tls mode direct but certificate source unavailable: " +
			"no database or MAPLE_CERT_ENC_KEY - all HTTPS handshakes will be rejected")
	} else {
		logger.Info("data plane tls mode",
			zap.String("mode", string(tlsMode)))
	}
	accessLog := logs.NewAccessLog(5000)
	errLog := logs.NewErrLog(2000)
	// ACME http-01 挑战代答（仅启用 ACME 时注入；nil 表示不拦截）。
	var acmeResponder gateway.ChallengeResponder
	if acmeChallenges != nil {
		acmeResponder = acmeChallenges
	}
	dp := gateway.NewDataPlane(gateway.DataPlaneConfig{
		Address:             httpAddr,
		HTTPSAddress:        httpsAddr,
		CertFile:            certFile,
		KeyFile:             keyFile,
		TLSMode:             tlsMode,
		TLSMinVersion:       cfg.TLS.MinVersion,
		GetCertificate:      sniGetter,
		EnforceSNIHostMatch: tlsMode == gateway.TLSModeDirect && cfg.TLS.EnforceSNIHostMatch,
		Resolver:            resolver,
		Transport:           transport,
		ReadHeaderTimeout:   px.ReadHeaderTimeout,
		ReadTimeout:         px.ReadTimeout,
		IdleTimeout:         px.IdleTimeout,
		MaxHeaderBytes:      px.MaxHeaderBytes,
		MaxBodyBytes:        px.MaxBodyBytes,
		MaxInFlight:         px.MaxInFlight,
		Logger:              logger,
		Metrics:             metricReg,
		AccessLog:           accessLog,
		ErrLog:              errLog,
		Tracer:              trc,
		ACMEChallenge:       acmeResponder,
		GatewayInstance:     instanceID,
		Node:                nodeName,
	})
	dpErrCh := dp.Start()

	// 跨实例运行时配置同步：本进程内的内存缓存在本进程 PATCH 后已刷新，但 HA 多实例下
	// 其它实例改的设置在本地不可见。周期重载（对齐路由缓存 AutoRebuild 模式）后把
	// 可热更项推送给各消费方，使变更最终一致（≤5s），无需引入 Redis 依赖。
	if setRepo != nil {
		go func() {
			ticker := time.NewTicker(5 * time.Second)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					if err := setRepo.Reload(ctx); err != nil {
						logger.Warn("runtime settings reload failed", zap.String("err", err.Error()))
						continue
					}
					h := runtimeHealth()
					p := runtimeProxy()
					dp.SetMaxInFlight(p.MaxInFlight)
					dp.SetMaxBodyBytes(p.MaxBodyBytes)
					if healthChecker != nil {
						healthChecker.SetConfig(health.Config{
							Interval:         h.Interval,
							Timeout:          h.Timeout,
							FailureThreshold: h.FailureThreshold,
							SuccessThreshold: h.SuccessThreshold,
							GracePeriod:      h.GracePeriod,
							Path:             "/health",
						})
					}
					// ACME 运行时配置：provider 配置（目录/邮箱/密钥类型/enabled）与续期参数
					// 均为热更；间隔变化触发循环唤醒即时应用。enabled 关闭时循环内部跳过扫描。
					if certSvc != nil {
						a := runtimeACME()
						certSvc.SetACMEConfig(certificate.ACMEConfig{
							Enabled:      a.Enabled,
							DirectoryURL: a.DirectoryURL,
							Email:        a.Email,
							Challenge:    a.Challenge,
							KeyType:      a.KeyType,
						})
						certSvc.SetRenewConfig(renewConfig())
						certSvc.SetRenewInterval(a.RenewCheckInterval)
					}
				}
			}
		}()
	}

	// Management API（数据平面与控制面分离）。
	uiDir := resolveUIDir(cfg.Management.UIDir)
	if uiDir != "" {
		logger.Info("management ui enabled", zap.String("dir", uiDir))
	}
	var mgmtApp *fiber.App
	if db != nil {
		mgmtApp = api.New(api.Deps{Ent: entClient, ReadyDB: db.SQL.PingContext, RouteCache: routeCache, Metrics: metricReg,
			AccessLog: accessLog, ErrLog: errLog, Settings: setRepo, Series: series,
			HA: ha.NewHandler(ha.NewRepository(entClient), coord), Certificates: certH, UIDir: uiDir,
			CMClient: cmClient, IsLeader: isLeader, InternalToken: cfg.Security.InternalToken})
	} else {
		mgmtApp = api.New(api.Deps{Ent: nil, Metrics: metricReg, AccessLog: accessLog, ErrLog: errLog, Series: series, UIDir: uiDir, InternalToken: cfg.Security.InternalToken})
	}
	go func() {
		logger.Info("management api listening", zap.String("addr", cfg.Addr(cfg.Management.Port)))
		if err := mgmtApp.Listen(cfg.Addr(cfg.Management.Port)); err != nil {
			logger.Error("management api listen failed", zap.String("err", err.Error()))
		}
	}()

	// 等 Fiber 打印完启动 banner 后再打进程状态汇总，避免输出顺序错乱。
	time.Sleep(150 * time.Millisecond)
	printStartupSummary(cfg.Addr(cfg.Management.Port), db, redisClient)

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
	// 等待后台签发任务结束，避免任务写入已关闭的 DB。
	if certSvc != nil {
		certSvc.WaitBackground()
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

// runMigration 连接 DB 并执行 SQL 迁移（建表/演进），不负责填充种子数据。
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
	pkg.Log().Info("migrations applied")
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
