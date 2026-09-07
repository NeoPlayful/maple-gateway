package dashboard

import (
	"github.com/NeoPlayful/maple-gateway/server/pkg"
	"github.com/gofiber/fiber/v3"
	"github.com/jackc/pgx/v5/pgxpool"
)

// StatFn 返回路由缓存命中/未命中统计（nil 时返回 0）。
type StatFn func() (hits, misses uint64)

// Handler 暴露 Dashboard 聚合接口。
type Handler struct {
	repo  *Repository
	stats StatFn
}

// NewHandler 构造。stats 为 nil 时缓存统计填 0。
func NewHandler(pool *pgxpool.Pool, stats StatFn) *Handler {
	return &Handler{
		repo:  NewRepository(pool),
		stats: stats,
	}
}

// Overview GET /api/admin/dashboard/overview
func (h *Handler) Overview(c fiber.Ctx) error {
	ov, err := h.repo.Overview(c.Context())
	if err != nil {
		return pkg.Err(c, err)
	}
	if h.stats != nil {
		ov.CacheHits, ov.CacheMisses = h.stats()
	}
	return pkg.OK(c, ov)
}
