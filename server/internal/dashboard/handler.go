package dashboard

import (
	"strconv"

	"github.com/NeoPlayful/maple-gateway/server/ent"
	"github.com/NeoPlayful/maple-gateway/server/pkg"
	"github.com/gofiber/fiber/v3"
)

// StatFn 返回路由缓存命中/未命中统计（nil 时返回 0）。
type StatFn func() (hits, misses uint64)

// Handler 暴露 Dashboard 聚合接口。
type Handler struct {
	repo   *Repository
	stats  StatFn
	series SeriesReader // 可空；nil 时序接口返回空
}

// NewHandler 构造。stats 为 nil 时缓存统计填 0。
// series 为数据平面时间桶（进程内趋势数据源）；nil 时时序接口返回空序列。
func NewHandler(client *ent.Client, stats StatFn, series SeriesReader) *Handler {
	return &Handler{
		repo:   NewRepository(client),
		stats:  stats,
		series: series,
	}
}

// windows 从 query ?minutes= 换算时间桶窗口数（默认最近 30 分钟，桶约 15s/个）。
func windows(c fiber.Ctx, defMinutes int) int {
	mins := defMinutes
	if raw := c.Query("minutes"); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			mins = n
		}
	}
	return mins * 60 / 15 // 15s 窗口；不精确但足够趋势展示
}

// Traffic GET /api/admin/dashboard/traffic?minutes=30
// 返回请求量/错误/限流命中/每秒请求/错误率 的时间序列。
func (h *Handler) Traffic(c fiber.Ctx) error {
	return pkg.OK(c, Traffic(h.series, windows(c, 30)))
}

// Latency GET /api/admin/dashboard/latency?minutes=30
// 返回各窗口平均与 p95 延迟（毫秒）序列。
func (h *Handler) Latency(c fiber.Ctx) error {
	return pkg.OK(c, Latency(h.series, windows(c, 30)))
}

// Errors GET /api/admin/dashboard/errors?minutes=30
// 错误（5xx+rejected）计数序列，前端可与 traffic 叠加。
func (h *Handler) Errors(c fiber.Ctx) error {
	return pkg.OK(c, h.errorSeries(windows(c, 30)))
}

// errorSeries 独立出错误序列（不含 rps 等总量字段）。
func (h *Handler) errorSeries(n int) []TrafficPoint {
	pts := Traffic(h.series, n)
	out := make([]TrafficPoint, 0, len(pts))
	for _, p := range pts {
		out = append(out, TrafficPoint{
			Time:      p.Time,
			Errors:    p.Errors,
			ErrorRate: p.ErrorRate,
		})
	}
	return out
}

// Instances GET /api/admin/dashboard/instances —— 实例健康/状态分布。
func (h *Handler) Instances(c fiber.Ctx) error {
	d, err := h.repo.instanceDist(c.Context())
	if err != nil {
		return pkg.Err(c, err)
	}
	return pkg.OK(c, d)
}

// Nodes GET /api/admin/dashboard/nodes —— 节点状态分布。
func (h *Handler) Nodes(c fiber.Ctx) error {
	d, err := h.repo.nodeDist(c.Context())
	if err != nil {
		return pkg.Err(c, err)
	}
	return pkg.OK(c, d)
}

// Canary GET /api/admin/dashboard/canary —— 进行中的 canary 列表。
func (h *Handler) Canary(c fiber.Ctx) error {
	list, err := h.repo.runningCanary(c.Context())
	if err != nil {
		return pkg.Err(c, err)
	}
	return pkg.OK(c, list)
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
