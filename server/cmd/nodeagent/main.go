// Node Agent 进程入口。
//
// 与 Gateway/Container Manager 同仓库、同 Go module，但作为独立进程运行在每一台 Node 上：
// 是唯一直连本机 Docker Engine 的组件，接收 CM 下发的容器操作指令并执行，
// 同时把本机容器与资源状态回报给 CM。详细设计见 docs/plan-phase7.md。
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/NeoPlayful/maple-gateway/server/internal/nodeagent/api"
	"github.com/NeoPlayful/maple-gateway/server/internal/nodeagent/config"
	"github.com/NeoPlayful/maple-gateway/server/internal/nodeagent/docker"
	"github.com/NeoPlayful/maple-gateway/server/internal/nodeagent/exec"
	"github.com/NeoPlayful/maple-gateway/server/internal/nodeagent/runtime"
	"github.com/NeoPlayful/maple-gateway/server/internal/nodeagent/wsclient"
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

	// 主动连接 CM：配置了 server_url 即建立 WebSocket，命令经此连接下行。
	if cfg.Agent.ServerURL != "" {
		startWSClient(ctx, cfg, rt, logger)
	}

	// 关闭面向 CM 的 HTTP 入站口：命令全部经 WS 下行，节点不暴露管理端口。
	// 必须已配置 server_url，否则节点没有任何可达通道可下发命令。
	if cfg.Agent.DisableHTTP {
		if cfg.Agent.ServerURL == "" {
			return fmt.Errorf("disable_http 需要同时配置 server_url，否则节点无可达通道")
		}
		logger.Info("node agent HTTP listener disabled; serving over WebSocket only")
		<-ctx.Done()
		logger.Info("node agent shutting down")
		return nil
	}

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

// startWSClient 装配并启动 WebSocket 客户端：从配置/磁盘取得注册资料，
// 把 CM 下发的 Action 映射到本机 runtime 执行。
func startWSClient(ctx context.Context, cfg *config.Config, rt *runtime.Runtime, logger *zap.Logger) {
	credPath := cfg.Agent.CredentialPath
	cred, err := wsclient.LoadCredential(credPath)
	if err != nil {
		logger.Warn("load credential failed", zap.Error(err))
	}
	enrollee := wsclient.NewStaticEnrollee(
		cfg.Agent.ServerURL, cfg.Agent.EnrollmentToken, cfg.Agent.NodeName, cred,
		func(c *wsclient.Credential) error {
			if credPath == "" {
				return nil
			}
			return wsclient.SaveCredential(credPath, c)
		},
	)
	client := wsclient.New(enrollee, exec.New(rt), wsclient.NewWSDialer(), logger)
	go client.Run(ctx)
	logger.Info("agent WebSocket client started", zap.String("server_url", cfg.Agent.ServerURL))
}
