package auth

import (
	"strings"

	"github.com/NeoPlayful/maple-gateway/server/pkg"
	"github.com/gofiber/fiber/v3"
)

// adminIDKey 是 context 中管理员 ID 的键。
const adminIDKey = "auth.adminID"

// Middleware 校验 Authorization: Bearer <token>，把 admin id 放入 Locals。
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
