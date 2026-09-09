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
	errLog *ErrLog // 可空；nil 时 /logs/error 返回空
}

// NewHandler 构造。errLog 可空。
func NewHandler(access *AccessLog, errLog *ErrLog) *Handler {
	return &Handler{access: access, errLog: errLog}
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

// parseLimitOffset 解析 limit/offset。
func parseLimitOffset(c fiber.Ctx) (limit, offset int) {
	limit, _ = strconv.Atoi(c.Query("limit", "100"))
	offset, _ = strconv.Atoi(c.Query("offset", "0"))
	if limit <= 0 || limit > 1000 {
		limit = 100
	}
	if offset < 0 {
		offset = 0
	}
	return limit, offset
}

// validateTimeRange 校验 from/to 查询参数，非法返回错误。
func validateTimeRange(c fiber.Ctx) (time.Time, bool, time.Time, bool, error) {
	from, hasFrom := parseTime(c.Query("from"))
	to, hasTo := parseTime(c.Query("to"))
	if c.Query("from") != "" && !hasFrom {
		return time.Time{}, false, time.Time{}, false, pkg.ErrValidation("from 须为 RFC3339 时间")
	}
	if c.Query("to") != "" && !hasTo {
		return time.Time{}, false, time.Time{}, false, pkg.ErrValidation("to 须为 RFC3339 时间")
	}
	return from, hasFrom, to, hasTo, nil
}

// Access GET /api/admin/logs/access?request_id=&host=&status=&from=&to=&limit=&offset=
func (h *Handler) Access(c fiber.Ctx) error {
	if h.access == nil {
		return pkg.OKMeta(c, []AccessEntry{}, fiber.Map{"total": 0, "count": 0})
	}
	limit, offset := parseLimitOffset(c)
	status, _ := strconv.Atoi(c.Query("status", "0"))
	from, _, to, _, err := validateTimeRange(c)
	if err != nil {
		return pkg.Err(c, err)
	}
	items := h.access.Query(c.Query("host"), status, c.Query("request_id"), from, to, limit, offset)
	return pkg.OKMeta(c, items, fiber.Map{"total": len(items), "count": h.access.Count(),
		"limit": limit, "offset": offset})
}

// Error GET /api/admin/logs/error?request_id=&host=&status=&from=&to=&limit=&offset=
func (h *Handler) Error(c fiber.Ctx) error {
	if h.errLog == nil {
		return pkg.OKMeta(c, []ErrEntry{}, fiber.Map{"total": 0, "count": 0})
	}
	limit, offset := parseLimitOffset(c)
	status, _ := strconv.Atoi(c.Query("status", "0"))
	from, _, to, _, err := validateTimeRange(c)
	if err != nil {
		return pkg.Err(c, err)
	}
	items := h.errLog.Query(c.Query("host"), status, c.Query("request_id"), from, to, limit, offset)
	return pkg.OKMeta(c, items, fiber.Map{"total": len(items), "count": h.errLog.Count(),
		"limit": limit, "offset": offset})
}
