// Package api 提供 Node Agent 面向 Container Manager 的 HTTP 接口。
//
// 面向 CM 的容器操作接口（node/info、containers、images/pull）与认证、来源限制
// 见 docs/plan-phase7.md。Agent 仅绑内网地址，且只操作带受管标签的容器。
package api

import (
	"net"
	"strings"

	"github.com/NeoPlayful/maple-gateway/server/internal/nodeagent/config"
	"github.com/NeoPlayful/maple-gateway/server/internal/nodeagent/docker"
	"github.com/NeoPlayful/maple-gateway/server/internal/nodeagent/runtime"
	"github.com/gofiber/fiber/v3"
	"go.uber.org/zap"
)

// New 构造 Node Agent 的 Fiber 应用。
func New(cfg *config.Config, logger *zap.Logger, rt *runtime.Runtime) *fiber.App {
	app := fiber.New(fiber.Config{
		AppName:   "maple-nodeagent",
		BodyLimit: 4 * 1024 * 1024,
	})
	app.Get("/health", func(c fiber.Ctx) error {
		return c.JSON(fiber.Map{"status": "ok", "service": "nodeagent"})
	})

	h := &handler{rt: rt, logger: logger}
	g := app.Group("/api/internal", authMiddleware(cfg.Agent.Token, cfg.Agent.AllowedCIDRs))
	g.Get("/node/info", h.nodeInfo)
	g.Get("/containers", h.listContainers)
	g.Post("/containers", h.createContainer)
	g.Post("/containers/:id/start", h.startContainer)
	g.Post("/containers/:id/stop", h.stopContainer)
	g.Delete("/containers/:id", h.removeContainer)
	g.Post("/images/pull", h.pullImage)

	return app
}

// handler 持有运行时与日志。
type handler struct {
	rt     *runtime.Runtime
	logger *zap.Logger
}

// authMiddleware 校验 CM 令牌并限制来源网段（AllowedCIDRs 为空则不限制来源）。
func authMiddleware(token string, allowedCIDRs []string) fiber.Handler {
	return func(c fiber.Ctx) error {
		if token == "" {
			return fiber.NewError(fiber.StatusUnauthorized, "agent token 未配置")
		}
		h := c.Get("Authorization")
		if !strings.HasPrefix(h, "Bearer ") || strings.TrimPrefix(h, "Bearer ") != token {
			return fiber.NewError(fiber.StatusUnauthorized, "无效的 agent token")
		}
		if len(allowedCIDRs) > 0 && !ipAllowed(c.IP(), allowedCIDRs) {
			return fiber.NewError(fiber.StatusForbidden, "来源地址不在允许网段")
		}
		return c.Next()
	}
}

// ipAllowed 判断来源 IP 是否落在允许网段内。
func ipAllowed(ipStr string, cidrs []string) bool {
	ip := net.ParseIP(ipStr)
	if ip == nil {
		return false
	}
	for _, cidr := range cidrs {
		_, network, err := net.ParseCIDR(cidr)
		if err != nil {
			continue
		}
		if network.Contains(ip) {
			return true
		}
	}
	return false
}

// nodeInfo GET /api/internal/node/info
func (h *handler) nodeInfo(c fiber.Ctx) error {
	info, err := h.rt.Info(c.Context())
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}
	return c.JSON(info)
}

// listContainers GET /api/internal/containers
func (h *handler) listContainers(c fiber.Ctx) error {
	items, err := h.rt.List(c.Context())
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}
	return c.JSON(items)
}

// createContainer POST /api/internal/containers
// 幂等键 = instance_id：同 instance_id 的受管容器已存在则不重建，直接返回。
func (h *handler) createContainer(c fiber.Ctx) error {
	var spec docker.CreateSpec
	if err := c.Bind().Body(&spec); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "请求体格式错误")
	}
	if spec.InstanceID == "" || spec.Image == "" {
		return fiber.NewError(fiber.StatusBadRequest, "缺少 instance_id 或 image")
	}
	id, hostPort, err := h.rt.Ensure(c.Context(), spec)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}
	return c.JSON(fiber.Map{"id": id, "instance_id": spec.InstanceID, "host_port": hostPort})
}

// startContainer POST /api/internal/containers/:id/start
func (h *handler) startContainer(c fiber.Ctx) error {
	if err := h.rt.Start(c.Context(), c.Params("id")); err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}
	return c.JSON(fiber.Map{"started": true})
}

// stopContainer POST /api/internal/containers/:id/stop
func (h *handler) stopContainer(c fiber.Ctx) error {
	if err := h.rt.Stop(c.Context(), c.Params("id")); err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}
	return c.JSON(fiber.Map{"stopped": true})
}

// removeContainer DELETE /api/internal/containers/:id?force=true
func (h *handler) removeContainer(c fiber.Ctx) error {
	force := c.Query("force", "false") == "true"
	if err := h.rt.Remove(c.Context(), c.Params("id"), force); err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}
	return c.JSON(fiber.Map{"removed": true})
}

// pullImage POST /api/internal/images/pull
func (h *handler) pullImage(c fiber.Ctx) error {
	var in struct {
		Image string `json:"image"`
	}
	if err := c.Bind().Body(&in); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "请求体格式错误")
	}
	if in.Image == "" {
		return fiber.NewError(fiber.StatusBadRequest, "缺少 image")
	}
	if err := h.rt.PullImage(c.Context(), in.Image); err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}
	return c.JSON(fiber.Map{"pulled": true})
}
