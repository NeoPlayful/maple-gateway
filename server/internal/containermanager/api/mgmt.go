package api

import (
	"strconv"

	"github.com/NeoPlayful/maple-gateway/server/internal/containermanager/desired"
	"github.com/NeoPlayful/maple-gateway/server/internal/containermanager/observer"
	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
)

// NodeStatus 是节点与 Agent 的对外视图。
type NodeStatus struct {
	Name       string            `json:"name"`
	Host       string            `json:"host"`
	Region     string            `json:"region"`
	Labels     map[string]string `json:"labels,omitempty"`
	Healthy    bool              `json:"healthy"`
	GatewayID  string            `json:"gateway_id,omitempty"`
	LastSeenMs int64             `json:"last_seen_ms"`
	// 节点容量与 Agent 元信息（来自 Agent /node/info；未采集到则为零值）。
	CPUs          int    `json:"cpus,omitempty"`
	MemoryBytes   int64  `json:"memory_bytes,omitempty"`
	DockerVersion string `json:"docker_version,omitempty"`
}

// ContainerStatus 是受管容器对管理端的视图（含所在节点）。
type ContainerStatus struct {
	InstanceID  string            `json:"instance_id"`
	ContainerID string            `json:"container_id"`
	Name        string            `json:"name"`
	Image       string            `json:"image"`
	State       string            `json:"state"`
	Status      string            `json:"status"`
	Labels      map[string]string `json:"labels,omitempty"`
	NodeName    string            `json:"node_name"`
	HostPort    int               `json:"host_port"`
	ExitCode    int               `json:"exit_code"`
	OOMKilled   bool              `json:"oom_killed"`
	FinishedAt  string            `json:"finished_at,omitempty"`
}

// Mgmt 汇总管理读接口所需的数据源（函数字段，便于从各子系统装配而无需额外适配器）。
// 任一字段为空则对应端点降级为空结果。
type Mgmt struct {
	Stats      func() observer.Stats
	Nodes      func() []NodeStatus
	Metrics    func() map[string]observer.NodeMetric
	Errors     func() []observer.RuntimeError
	Phases     func() map[uuid.UUID]desired.Phase
	Containers func() []ContainerStatus
	Restart    func(instanceID string) error
	Stop       func(instanceID string) error
	Start      func(instanceID string) error
	Logs       func(instanceID string, tail int) (string, error)
}

// registerMgmt 挂载管理读接口与人工控制接口（内部令牌认证，供 Gateway 聚合代理调用）。
func registerMgmt(app *fiber.App, token string, m Mgmt) {
	g := app.Group("/api/internal/mgmt", internalAuth(token))

	// 观测统计 + 部署进度：管理面总览。
	g.Get("/overview", func(c fiber.Ctx) error {
		out := fiber.Map{}
		if m.Stats != nil {
			out["stats"] = m.Stats()
		}
		if m.Phases != nil {
			out["deployments"] = m.Phases()
		}
		if m.Nodes != nil {
			out["nodes"] = m.Nodes()
		}
		return c.JSON(out)
	})

	g.Get("/nodes", func(c fiber.Ctx) error {
		if m.Nodes == nil {
			return c.JSON([]NodeStatus{})
		}
		return c.JSON(m.Nodes())
	})

	g.Get("/metrics", func(c fiber.Ctx) error {
		if m.Metrics == nil {
			return c.JSON(fiber.Map{})
		}
		return c.JSON(m.Metrics())
	})

	g.Get("/errors", func(c fiber.Ctx) error {
		if m.Errors == nil {
			return c.JSON([]observer.RuntimeError{})
		}
		return c.JSON(m.Errors())
	})

	// 受管容器清单：供管理端列表化运行时容器并提供行内操作。
	g.Get("/containers", func(c fiber.Ctx) error {
		if m.Containers == nil {
			return c.JSON([]ContainerStatus{})
		}
		return c.JSON(m.Containers())
	})

	// 人工控制：实例 start/stop/restart 与日志查看。
	g.Post("/instances/:id/restart", func(c fiber.Ctx) error {
		if m.Restart == nil {
			return fiber.NewError(fiber.StatusNotImplemented, "未启用人工控制")
		}
		if err := m.Restart(c.Params("id")); err != nil {
			return fiber.NewError(fiber.StatusBadGateway, err.Error())
		}
		return c.JSON(fiber.Map{"restarted": true})
	})

	g.Post("/instances/:id/stop", func(c fiber.Ctx) error {
		if m.Stop == nil {
			return fiber.NewError(fiber.StatusNotImplemented, "未启用人工控制")
		}
		if err := m.Stop(c.Params("id")); err != nil {
			return fiber.NewError(fiber.StatusBadGateway, err.Error())
		}
		return c.JSON(fiber.Map{"stopped": true})
	})

	g.Post("/instances/:id/start", func(c fiber.Ctx) error {
		if m.Start == nil {
			return fiber.NewError(fiber.StatusNotImplemented, "未启用人工控制")
		}
		if err := m.Start(c.Params("id")); err != nil {
			return fiber.NewError(fiber.StatusBadGateway, err.Error())
		}
		return c.JSON(fiber.Map{"started": true})
	})

	g.Get("/instances/:id/logs", func(c fiber.Ctx) error {
		if m.Logs == nil {
			return fiber.NewError(fiber.StatusNotImplemented, "未启用日志查看")
		}
		tail, _ := strconv.Atoi(c.Query("tail", "200"))
		logs, err := m.Logs(c.Params("id"), tail)
		if err != nil {
			return fiber.NewError(fiber.StatusBadGateway, err.Error())
		}
		return c.JSON(fiber.Map{"logs": logs})
	})
}
