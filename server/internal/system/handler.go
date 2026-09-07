// Package system 提供网关自身的健康与信息接口。
package system

import (
	"context"
	"runtime"
	"time"

	"github.com/NeoPlayful/maple-gateway/server/pkg"
	"github.com/gofiber/fiber/v3"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Handler 是系统接口处理器。
type Handler struct {
	pool *pgxpool.Pool
}

// NewHandler 构造。pool 为 nil 表示无 DB（无 DB 部署时 ready 报 not-ready）。
func NewHandler(pool *pgxpool.Pool) *Handler {
	return &Handler{pool: pool}
}

// Health GET /api/system/health —— 进程存活即 ok。
func (h *Handler) Health(c fiber.Ctx) error {
	return c.JSON(fiber.Map{"status": "ok"})
}

// Live GET /api/system/live —— 存活探针。
func (h *Handler) Live(c fiber.Ctx) error {
	return c.JSON(fiber.Map{"status": "alive"})
}

// Ready GET /api/system/ready —— DB 就绪才 ready。
func (h *Handler) Ready(c fiber.Ctx) error {
	if h.pool == nil {
		return c.Status(503).JSON(fiber.Map{"status": "not_ready", "reason": "no database"})
	}
	ctx, cancel := context.WithTimeout(c.Context(), 2*time.Second)
	defer cancel()
	if err := h.pool.Ping(ctx); err != nil {
		return c.Status(503).JSON(fiber.Map{"status": "not_ready", "reason": "database unreachable"})
	}
	return c.JSON(fiber.Map{"status": "ready"})
}

// Info GET /api/admin/system/info —— 版本/运行信息。
func (h *Handler) Info(c fiber.Ctx) error {
	return pkg.OK(c, fiber.Map{
		"name":    "maple-gateway",
		"version": Version,
		"go":      runtime.Version(),
		"runtime": runtime.GOOS + "/" + runtime.GOARCH,
	})
}

// Version 构建版本号，未来通过 ldflags 注入。
var Version = "0.1.0-dev"
