package api

import (
	"github.com/NeoPlayful/maple-gateway/server/internal/cache"
	"github.com/NeoPlayful/maple-gateway/server/pkg"
	"github.com/gofiber/fiber/v3"
)

// routeCacheHandler 暴露路由缓存查看与重建接口（辅助排障）。
type routeCacheHandler struct {
	cache *cache.Cache
}

// ListRoutes GET /api/admin/routes
func (h *routeCacheHandler) ListRoutes(c fiber.Ctx) error {
	return pkg.OK(c, h.cache.Entries())
}

// Status GET /api/admin/cache/status
func (h *routeCacheHandler) Status(c fiber.Ctx) error {
	return pkg.OK(c, fiber.Map{
		"enabled": true,
		"routes":  len(h.cache.Entries()),
	})
}

// Stats GET /api/admin/cache/stats
func (h *routeCacheHandler) Stats(c fiber.Ctx) error {
	hits, misses := h.cache.Stats()
	return pkg.OK(c, fiber.Map{"hits": hits, "misses": misses})
}

// Rebuild POST /api/admin/cache/rebuild
func (h *routeCacheHandler) Rebuild(c fiber.Ctx) error {
	if err := h.cache.Rebuild(c.Context()); err != nil {
		return pkg.Err(c, err)
	}
	return pkg.OK(c, fiber.Map{"rebuild": true, "routes": len(h.cache.Entries())})
}
