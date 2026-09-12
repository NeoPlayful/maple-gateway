// Package discovery 提供 Container Manager / Node Agent 的状态上报 Internal API。
// 独立于 admin Bearer 认证，使用 internal token（取自 config 的 security.internal_token，
// 回退环境变量 MAPLE_INTERNAL_TOKEN）；不对公网开放。
package discovery

import (
	"strings"

	"github.com/NeoPlayful/maple-gateway/server/pkg"
	"github.com/gofiber/fiber/v3"
)

// Middleware 校验 Internal Token（Authorization: Bearer <token>）。
func Middleware(token string) fiber.Handler {
	return func(c fiber.Ctx) error {
		if token == "" {
			return pkg.Err(c, pkg.ErrUnauthorized("internal token 未配置"))
		}
		h := c.Get("Authorization")
		if !strings.HasPrefix(h, "Bearer ") || !strings.EqualFold(strings.TrimPrefix(h, "Bearer "), token) {
			return pkg.Err(c, pkg.ErrUnauthorized("无效的 internal token"))
		}
		return c.Next()
	}
}
