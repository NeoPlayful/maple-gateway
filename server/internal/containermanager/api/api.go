// Package api 提供 Container Manager 的对外 HTTP 接口。
// 面向 Gateway：接收部署意图、返回编排状态；面向 Agent：下发容器操作。
//
// 部署意图接收端点已挂载（进程内留档，验证 Gateway → CM 下发通道）；
// 期望态持久化、对账与容器操作下发随编排能力逐步接入。
package api

import (
	"github.com/NeoPlayful/maple-gateway/server/internal/containermanager/config"
	"github.com/NeoPlayful/maple-gateway/server/internal/containermanager/desired"
	"github.com/NeoPlayful/maple-gateway/server/internal/containermanager/observer"
	"github.com/gofiber/fiber/v3"
	"go.uber.org/zap"
)

// StatsProvider 提供 CM 集成健康统计（观测上报滞后/错误/纳管数）。
type StatsProvider interface {
	Stats() observer.Stats
}

// New 构造 CM 的 Fiber 应用。store 保存期望态；stats 可空（提供 /api/internal/stats）。
func New(cfg *config.Config, logger *zap.Logger, store *desired.Store, stats StatsProvider) *fiber.App {
	app := fiber.New(fiber.Config{
		AppName:   "maple-cm",
		BodyLimit: 4 * 1024 * 1024,
	})
	app.Get("/health", func(c fiber.Ctx) error {
		return c.JSON(fiber.Map{"status": "ok", "service": "cm"})
	})

	registerDeployments(app, store, cfg.CM.Token)
	if stats != nil {
		registerStats(app, stats)
	}

	_ = cfg
	_ = logger
	return app
}
