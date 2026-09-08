package api

import (
	"context"
	"strconv"
	"strings"
	"time"

	"entgo.io/ent/dialect/sql"
	"github.com/NeoPlayful/maple-gateway/server/ent"
	entaudit "github.com/NeoPlayful/maple-gateway/server/ent/auditlog"
	"github.com/NeoPlayful/maple-gateway/server/internal/auth"
	"github.com/NeoPlayful/maple-gateway/server/pkg"
	"github.com/gofiber/fiber/v3"
	"github.com/jackc/pgx/v5/pgxpool"
)

// auditMiddleware 把 admin 组的写操作（非 GET/HEAD）记入 audit_logs。
// 记录请求成功（<400）后执行；写操作低频，同步落库即可。
func auditMiddleware(pool *pgxpool.Pool) fiber.Handler {
	return func(c fiber.Ctx) error {
		err := c.Next()

		method := c.Method()
		if method == fiber.MethodGet || method == fiber.MethodHead {
			return err
		}
		if c.Response() == nil || c.Response().StatusCode() >= 400 {
			return err
		}

		path := c.Path()
		ctx, cancel := context.WithTimeout(c.Context(), 2*time.Second)
		defer cancel()
		_, _ = pool.Exec(ctx,
			`INSERT INTO audit_logs(admin_id, action, target_type, target_id, ip)
			 VALUES($1,$2,$3,$4,$5)`,
			nullStr(auth.AdminID(c)),
			method+" "+path,
			resourceFromPath(path),
			segmentFromPath(path),
			c.IP(),
		)
		return err
	}
}

// resourceFromPath 取路径第一资源段，如 /api/admin/tenants/:id → tenants。
func resourceFromPath(path string) string {
	parts := strings.Split(path, "/")
	for _, p := range parts {
		if p != "" && p != "api" && p != "admin" {
			return p
		}
	}
	return path
}

// segmentFromPath 取最后一个非动作路径段（尽力近似对象 ID，非关键）。
func segmentFromPath(path string) string {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	for i := len(parts) - 1; i >= 0; i-- {
		p := parts[i]
		if p == "" || p == "api" || p == "admin" || p == "register" {
			continue
		}
		// 跳过动作词
		switch p {
		case "enable", "disable", "suspend", "drain", "undrain", "health",
			"rebuild", "reload", "me", "logout", "login":
			continue
		}
		return p
	}
	return ""
}

func nullStr(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// auditLogsHandler 查询 audit_logs 表（分页 + action/时间过滤，基于 Ent）。
type auditLogsHandler struct {
	ent *ent.Client
}

// auditRow 是审计日志查询行。
type auditRow struct {
	ID         string    `json:"id"`
	AdminID    *string   `json:"admin_id,omitempty"`
	Action     string    `json:"action"`
	TargetType string    `json:"target_type"`
	TargetID   *string   `json:"target_id,omitempty"`
	IP         *string   `json:"ip,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
}

// List GET /api/admin/logs/audit?action=&target_type=&from=&to=&limit=&offset=
func (h *auditLogsHandler) List(c fiber.Ctx) error {
	limit, _ := strconv.Atoi(c.Query("limit", "100"))
	offset, _ := strconv.Atoi(c.Query("offset", "0"))
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	if offset < 0 {
		offset = 0
	}
	action := c.Query("action")
	targetType := c.Query("target_type")
	from, hasFrom := parseTimeRFC3339(c.Query("from"))
	to, hasTo := parseTimeRFC3339(c.Query("to"))
	if c.Query("from") != "" && !hasFrom {
		return pkg.Err(c, pkg.ErrValidation("from 须为 RFC3339 时间"))
	}
	if c.Query("to") != "" && !hasTo {
		return pkg.Err(c, pkg.ErrValidation("to 须为 RFC3339 时间"))
	}

	q := h.ent.AuditLog.Query()
	if action != "" {
		q = q.Where(entaudit.ActionHasPrefix(action))
	}
	if targetType != "" {
		q = q.Where(entaudit.TargetTypeEQ(targetType))
	}
	if hasFrom {
		q = q.Where(entaudit.CreatedAtGTE(from))
	}
	if hasTo {
		q = q.Where(entaudit.CreatedAtLTE(to))
	}
	total, err := q.Count(c.Context())
	if err != nil {
		return pkg.Err(c, err)
	}
	es, err := q.
		Order(entaudit.ByCreatedAt(sql.OrderDesc())).
		Limit(limit).
		Offset(offset).
		All(c.Context())
	if err != nil {
		return pkg.Err(c, err)
	}
	out := make([]auditRow, 0, len(es))
	for _, e := range es {
		r := auditRow{
			ID:         e.ID.String(),
			Action:     e.Action,
			TargetType: e.TargetType,
			CreatedAt:  e.CreatedAt,
		}
		if e.AdminID != nil {
			s := e.AdminID.String()
			r.AdminID = &s
		}
		r.TargetID = e.TargetID
		r.IP = e.IP
		out = append(out, r)
	}
	return pkg.OKMeta(c, out, fiber.Map{"total": total, "count": len(out), "limit": limit, "offset": offset})
}

// parseTimeRFC3339 解析 RFC3339 时间，空返回 (zero,false)。
func parseTimeRFC3339(s string) (time.Time, bool) {
	if s == "" {
		return time.Time{}, false
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}, false
	}
	return t, true
}
