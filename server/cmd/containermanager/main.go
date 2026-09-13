// Container Manager 进程入口。
//
// 与 Gateway（cmd/server）在同一仓库、同一 Go module，但作为独立进程运行：
// 接收 Gateway 下发的部署意图，调度并驱动各节点 Node Agent 创建/销毁容器，
// 再把节点与容器状态上报回 Gateway。详细设计见 docs/plan-phase7.md。
package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"sort"
	"syscall"
	"time"

	"github.com/NeoPlayful/maple-gateway/server/internal/agentprotocol"
	"github.com/NeoPlayful/maple-gateway/server/internal/containermanager/agentconn"
	"github.com/NeoPlayful/maple-gateway/server/internal/containermanager/agentregistry"
	"github.com/NeoPlayful/maple-gateway/server/internal/containermanager/applications"
	"github.com/NeoPlayful/maple-gateway/server/internal/containermanager/api"
	"github.com/NeoPlayful/maple-gateway/server/internal/containermanager/config"
	"github.com/NeoPlayful/maple-gateway/server/internal/containermanager/control"
	"github.com/NeoPlayful/maple-gateway/server/internal/containermanager/desired"
	"github.com/NeoPlayful/maple-gateway/server/internal/containermanager/enrollment"
	"github.com/NeoPlayful/maple-gateway/server/internal/containermanager/gwclient"
	"github.com/NeoPlayful/maple-gateway/server/internal/containermanager/logstream"
	"github.com/NeoPlayful/maple-gateway/server/internal/containermanager/nodes"
	"github.com/NeoPlayful/maple-gateway/server/internal/containermanager/observer"
	"github.com/NeoPlayful/maple-gateway/server/internal/containermanager/reconciler"
	"github.com/NeoPlayful/maple-gateway/server/internal/containermanager/tasksys"
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

	// 持久化（可选）：配置了 database_url 则节点/令牌/任务/期望态落库。
	var sqlDB *sql.DB
	if cfg.CM.DatabaseURL != "" {
		db, err := pkg.NewDB(ctx, cfg.CM.DatabaseURL)
		if err != nil {
			return fmt.Errorf("connect database: %w", err)
		}
		defer db.Close()
		sqlDB = db.SQL
	} else {
		logger.Warn("cm.database_url 未配置，节点/令牌/任务/期望态仅存内存（重启即失忆）")
	}

	// 统一节点模型（运行期）+ 上报 Gateway 客户端。
	nodeStore := nodes.New(sqlDB, 0, 0)
	if err := nodeStore.Load(ctx); err != nil {
		return fmt.Errorf("load nodes: %w", err)
	}
	go nodeStore.Runner(ctx, 15*time.Second)

	gw := gwclient.NewGatewayClient(cfg.CM.GatewayBaseURL, cfg.CM.GatewayToken)

	// 期望态存储 + 观测/对账循环 + 节点命令通道（WS 任务）。
	store := desired.NewStore(sqlDB)
	if err := store.Load(ctx); err != nil {
		return fmt.Errorf("load deployments: %w", err)
	}
	hub := agentconn.NewHub()
	streams := logstream.NewHub()
	tasks := tasksys.New(hub, cfg.CM.TaskTimeout, logger).WithStore(tasksys.NewStore(sqlDB))
	if err := tasks.Load(ctx); err != nil {
		return fmt.Errorf("load tasks: %w", err)
	}
	// Compose 应用存储与控制（整包多服务部署）。
	appStore := applications.NewStore(sqlDB)
	if err := appStore.Load(ctx); err != nil {
		return fmt.Errorf("load applications: %w", err)
	}
	registry := agentregistry.New(cfg.CM.Nodes, tasks, nodeStore)
	obs := observer.New(registry, gw, cfg.CM.ObserveInterval, logger)
	rec := reconciler.New(store, obs, registry, gw, cfg.CM.ReconcileInterval, logger)
	appCtl := applications.NewController(appStore, registry, logger)
	go obs.Run(ctx)
	go rec.Run(ctx)

	// Agent 主动连入：会话注册表 + 准入 + 任务系统。
	// 节点主动连 CM，CM 不向节点发起入站；命令经这条连接下行。
	tokenStore := enrollment.NewTokenStoreDB(sqlDB)
	if err := tokenStore.Load(ctx); err != nil {
		return fmt.Errorf("load enrollment tokens: %w", err)
	}
	enroll := enrollment.NewManagerWithTokens(cfg.CM.EnrollmentRequired, nodeStore, gw, tokenStore)
	agentSrv := agentconn.NewServer(cfg.CM.AgentListen, enroll, hub, agentconn.Callbacks{
		OnSessionStart: func(nodeID string) {
			nodeStore.Touch(nodeID)
			// 依节点登记名把视图绑定到 Gateway 节点 UUID（node_id）。
			if r, ok := nodeStore.Get(nodeID); ok && r.Name != "" {
				registry.Bind(r.Name, nodeID)
			}
		},
		OnSessionEnd: func(nodeID string, _ bool) { nodeStore.Disconnect(nodeID) },
		OnHeartbeat:  func(nodeID string, _ agentprotocol.HeartbeatPayload) { nodeStore.Touch(nodeID) },
		OnTaskAck:    func(nodeID string, p agentprotocol.TaskAckPayload) { tasks.OnAck(nodeID, p.TaskID) },
		OnTaskProgress: func(nodeID string, p agentprotocol.TaskProgressPayload) {
			tasks.OnProgress(nodeID, p.TaskID, p.Percent, p.Message)
		},
		OnTaskResult: func(nodeID string, p agentprotocol.TaskResultPayload) {
			tasks.OnResult(nodeID, p.TaskID, p.Status, p.Error, p.Result)
		},
		OnLogsData: func(nodeID string, p agentprotocol.LogsDataPayload) {
			streams.Dispatch(p.StreamID, p.Data, p.EOF)
		},
		OnDockerEvent: func(nodeID string, p agentprotocol.DockerEventPayload) {
			obs.OnDockerEvent(nodeID, p)
		},
	}, logger)
	go func() {
		if err := agentSrv.Serve(ctx); err != nil {
			logger.Error("agent websocket server stopped", zap.Error(err))
		}
	}()
	go tasks.RunSweeper(ctx.Done(), 15*time.Second)

	logger.Info("container manager observing nodes",
		zap.Int("nodes", len(registry.All())),
		zap.Bool("gateway_report_enabled", gw.Enabled()),
		zap.String("agent_listen", cfg.CM.AgentListen))

	// 人工控制通道：管理端经 Gateway 下发的实例操作（与对账器自动决策区分）。
	ctrl := control.New(registry, obs, store, streams, logger)
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
					Healthy: n.Online(), GatewayID: n.ID(),
				}
				if r, ok := nodeStore.Get(n.ID()); ok {
					st.LastSeenMs = r.LastSeenAt.UnixMilli()
				}
				if info, ok := infos[n.Name]; ok {
					st.CPUs = info.CPUs
					st.MemoryBytes = info.MemoryBytes
					st.DockerVersion = info.DockerVersion
					st.DockerAPIVer = info.DockerAPIVer
				}
				out = append(out, st)
			}
			return out
		},
		Restart: func(id string) error { return ctrl.Restart(ctx, id) },
		Stop:    func(id string) error { return ctrl.Stop(ctx, id) },
		Start:   func(id string) error { return ctrl.Start(ctx, id) },
		Logs:    func(id string, tail int) (string, error) { return ctrl.Logs(ctx, id, tail) },
		FollowLogs: func(cctx context.Context, id string, tail int) (<-chan []byte, <-chan struct{}, func() bool, error) {
			st, err := ctrl.FollowLogs(cctx, id, tail)
			if err != nil {
				return nil, nil, nil, err
			}
			return st.Chunks(), st.Done(), st.Overflow, nil
		},
		Events:  obs.Events,
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
			// 观测快照是映射表，遍历顺序随机；按 节点名 → 容器名 → 实例 ID 稳定排序，
			// 避免前端列表每次刷新都跳序。
			sort.Slice(out, func(i, j int) bool {
				if out[i].NodeName != out[j].NodeName {
					return out[i].NodeName < out[j].NodeName
				}
				if out[i].Name != out[j].Name {
					return out[i].Name < out[j].Name
				}
				return out[i].InstanceID < out[j].InstanceID
			})
			return out
		},
	}
	app := api.New(cfg, logger, store, obs, mgmt, tokenStore, api.TaskSource{
		List:   tasks.List,
		Get:    tasks.Get,
		Retry:  tasks.Retry,
		Cancel: tasks.AdminCancel,
	}, api.AppSource{
		List:     appStore.List,
		Get:      appStore.Get,
		Put:      appStore.Put,
		Delete:   appStore.Delete,
		Deploy:   appCtl.Deploy,
		Stop:     appCtl.Stop,
		Start:    appCtl.Start,
		Restart:  appCtl.Restart,
		Remove:   appCtl.Remove,
		Ps:       appCtl.Ps,
		Validate: appCtl.Validate,
	})

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
