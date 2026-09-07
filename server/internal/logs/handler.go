package logs

import (
	"strconv"
	"time"

	"github.com/NeoPlayful/maple-gateway/server/pkg"
	"github.com/gofiber/fiber/v3"
)

// Handler 暴露日志查询接口。
type Handler struct {
	access *AccessLog
}

// NewHandler 构造。
func NewHandler(access *AccessLog) *Handler {
	return &Handler{access: access}
}

// parseTime 解析 RFC3339 或空。
func parseTime(s string) (time.Time, bool) {
	if s == "" {
		return time.Time{}, false
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}, false
	}
	return t, true
}

// Access GET /api/admin/logs/access?host=&status=&from=&to=&limit=&offset=
func (h *Handler) Access(c fiber.Ctx) error {
	limit, _ := strconv.Atoi(c.Query("limit", "100"))
	offset, _ := strconv.Atoi(c.Query("offset", "0"))
	if limit <= 0 || limit > 1000 {
		limit = 100
	}
	if offset < 0 {
		offset = 0
	}
	status, _ := strconv.Atoi(c.Query("status", "0"))
	from, hasFrom := parseTime(c.Query("from"))
	to, hasTo := parseTime(c.Query("to"))
	if c.Query("from") != "" && !hasFrom {
		return pkg.Err(c, pkg.ErrValidation("from 须为 RFC3339 时间"))
	}
	if c.Query("to") != "" && !hasTo {
		return pkg.Err(c, pkg.ErrValidation("to 须为 RFC3339 时间"))
	}
	items := h.access.Query(c.Query("host"), status, from, to, limit, offset)
	return pkg.OKMeta(c, items, fiber.Map{"total": len(items), "count": h.access.Count(),
		"limit": limit, "offset": offset})
}
