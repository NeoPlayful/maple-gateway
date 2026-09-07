package api

import (
	"context"
	"strings"
	"time"

	"github.com/NeoPlayful/maple-gateway/server/internal/auth"
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
