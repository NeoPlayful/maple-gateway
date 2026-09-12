// Container Manager 进程入口。
//
// 与 Gateway（cmd/server）在同一仓库、同一 Go module，但作为独立进程运行：
// 接收 Gateway 下发的部署意图，调度并驱动各节点 Node Agent 创建/销毁容器，
// 再把节点与容器状态上报回 Gateway。详细设计见 docs/plan-phase7.md。
package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/NeoPlayful/maple-gateway/server/internal/containermanager/agentregistry"
	"github.com/NeoPlayful/maple-gateway/server/internal/containermanager/api"
	"github.com/NeoPlayful/maple-gateway/server/internal/containermanager/config"
	"github.com/NeoPlayful/maple-gateway/server/internal/containermanager/control"
	"github.com/NeoPlayful/maple-gateway/server/internal/containermanager/desired"
	"github.com/NeoPlayful/maple-gateway/server/internal/containermanager/gwclient"
	"github.com/NeoPlayful/maple-gateway/server/internal/containermanager/observer"
	"github.com/NeoPlayful/maple-gateway/server/internal/containermanager/reconciler"
	"github.com/NeoPlayful/maple-gateway/server/pkg"
	"github.com/joho/godotenv"
	"go.uber.org/zap"
)

func main() {
	configPath := flag.String("config", "", "config file path (YAML)")
	flag.Parse()

	_ = godotenv.Load()

	if err := run(*configPath); err != nil {
		log.Fatalf("containermanager: %v", err)
	}
}

func run(configPath string) error {
	cfg, err := config.Load(configPath)
	if err != nil {
		return err
	}
	if err := pkg.InitLogger(cfg.Logging.Level, cfg.Logging.Pretty); err != nil {
		return err
	}
	defer pkg.SyncLogger()
	logger := pkg.Log().With(zap.String("service", "cm"))

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// 期望态存储 + 节点注册表 + 上报 Gateway 客户端 + 观测/对账循环。
	store := desired.NewStore()
	registry := agentregistry.New(cfg.CM.Nodes)
	gw := gwclient.NewGatewayClient(cfg.CM.GatewayBaseURL, cfg.CM.GatewayToken)
	obs := observer.New(registry, gw, cfg.CM.ObserveInterval, logger)
	rec := reconciler.New(store, obs, registry, gw, cfg.CM.ReconcileInterval, logger)
	go obs.Run(ctx)
	go rec.Run(ctx)
	logger.Info("container manager observing nodes",
		zap.Int("nodes", len(registry.All())),
		zap.Bool("gateway_report_enabled", gw.Enabled()))

	// 人工控制通道：管理端经 Gateway 下发的实例操作（与对账器自动决策区分）。
	ctrl := control.New(registry, obs, store, logger)
	mgmt := api.Mgmt{
		Stats:   obs.Stats,
		Metrics: obs.Metrics,
		Errors:  obs.RuntimeErrors,
		Phases:  store.AllPhases,
		Nodes: func() []api.NodeStatus {
			nodes := registry.All()
			infos := obs.Infos()
			out := make([]api.NodeStatus, 0, len(nodes))
			for _, n := range nodes {
				st := api.NodeStatus{
					Name: n.Name, Host: n.Host, Region: n.Region, Labels: n.Labels,
					Healthy: n.Healthy, GatewayID: n.GatewayID, LastSeenMs: n.LastSeenMs,
				}
				if info, ok := infos[n.Name]; ok {
					st.CPUs = info.CPUs
					st.MemoryBytes = info.MemoryBytes
					st.DockerVersion = info.DockerVersion
				}
				out = append(out, st)
			}
			return out
		},
		Restart: func(id string) error { return ctrl.Restart(ctx, id) },
		Stop:    func(id string) error { return ctrl.Stop(ctx, id) },
		Start:   func(id string) error { return ctrl.Start(ctx, id) },
		Logs:    func(id string, tail int) (string, error) { return ctrl.Logs(ctx, id, tail) },
		Containers: func() []api.ContainerStatus {
			snap := obs.Snapshot()
			out := make([]api.ContainerStatus, 0, len(snap))
			for _, oc := range snap {
				c := oc.Container
				out = append(out, api.ContainerStatus{
					InstanceID:  c.InstanceID,
					ContainerID: c.ID,
					Name:        c.Name,
					Image:       c.Image,
					State:       c.State,
					Status:      c.Status,
					Labels:      c.Labels,
					NodeName:    oc.NodeName,
					HostPort:    c.HostPort,
					ExitCode:    c.ExitCode,
					OOMKilled:   c.OOMKilled,
					FinishedAt:  c.FinishedAt,
				})
			}
			return out
		},
	}
	app := api.New(cfg, logger, store, obs, mgmt)

	errCh := make(chan error, 1)
	go func() {
		logger.Info("container manager listening", zap.String("addr", cfg.CM.Listen))
		if err := app.Listen(cfg.CM.Listen); err != nil {
			errCh <- err
		}
	}()

	select {
	case <-ctx.Done():
		logger.Info("container manager shutting down")
		return app.Shutdown()
	case err := <-errCh:
		return err
	}
}
