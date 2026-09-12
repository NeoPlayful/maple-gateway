// Node Agent 进程入口。
//
// 与 Gateway/Container Manager 同仓库、同 Go module，但作为独立进程运行在每一台 Node 上：
// 是唯一直连本机 Docker Engine 的组件，接收 CM 下发的容器操作指令并执行，
// 同时把本机容器与资源状态回报给 CM。详细设计见 docs/plan-phase7.md。
package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/NeoPlayful/maple-gateway/server/internal/nodeagent/api"
	"github.com/NeoPlayful/maple-gateway/server/internal/nodeagent/config"
	"github.com/NeoPlayful/maple-gateway/server/internal/nodeagent/docker"
	"github.com/NeoPlayful/maple-gateway/server/internal/nodeagent/runtime"
	"github.com/NeoPlayful/maple-gateway/server/pkg"
	"github.com/joho/godotenv"
	"go.uber.org/zap"
)

func main() {
	configPath := flag.String("config", "", "config file path (YAML)")
	flag.Parse()

	_ = godotenv.Load()

	if err := run(*configPath); err != nil {
		log.Fatalf("nodeagent: %v", err)
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
	logger := pkg.Log().With(zap.String("service", "nodeagent"), zap.String("node", cfg.Agent.NodeName))

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// 接入本机 Docker Engine：Agent 是唯一直连 Docker 的组件。
	dcli, err := docker.New(cfg.Agent.DockerHost, cfg.Agent.ManagedLabel)
	if err != nil {
		return err
	}
	defer func() { _ = dcli.Close() }()
	logger.Info("docker engine connected", zap.String("host", cfg.Agent.DockerHost),
		zap.String("managed_label", cfg.Agent.ManagedLabel))

	rt := runtime.New(dcli, cfg.Agent.AllowedImages)
	app := api.New(cfg, logger, rt)

	errCh := make(chan error, 1)
	go func() {
		logger.Info("node agent listening", zap.String("addr", cfg.Agent.Listen))
		if err := app.Listen(cfg.Agent.Listen); err != nil {
			errCh <- err
		}
	}()

	select {
	case <-ctx.Done():
		logger.Info("node agent shutting down")
		return app.Shutdown()
	case err := <-errCh:
		return err
	}
}
