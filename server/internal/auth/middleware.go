package auth

import (
	"strings"

	"github.com/NeoPlayful/maple-gateway/server/pkg"
	"github.com/gofiber/fiber/v3"
)

// adminIDKey / roleKey 是 context 中管理员身份的两个键。
const (
	adminIDKey = "auth.adminID"
	roleKey    = "auth.role"
)

// Middleware 校验 Authorization: Bearer <token>，把 admin id 与 role 放入 Locals。
func Middleware(svc *Service) fiber.Handler {
	return func(c fiber.Ctx) error {
		h := c.Get("Authorization")
		if !strings.HasPrefix(h, "Bearer ") {
			return pkg.Err(c, pkg.ErrUnauthorized("缺少访问令牌"))
		}
		claims, err := svc.Parse(strings.TrimPrefix(h, "Bearer "))
		if err != nil {
			return pkg.Err(c, err)
		}
		c.Locals(adminIDKey, claims.AdminID)
		c.Locals(roleKey, claims.Role)
		return c.Next()
	}
}

// AdminID 读取中间件写入的管理员 ID。
func AdminID(c fiber.Ctx) string {
	if v, ok := c.Locals(adminIDKey).(string); ok {
		return v
	}
	return ""
}

// Role 读取中间件写入的管理员角色；缺失（旧 token）按 super_admin 兜底。
func Role(c fiber.Ctx) string {
	if v, ok := c.Locals(roleKey).(string); ok && v != "" {
		return v
	}
	return RoleSuperAdmin
}
