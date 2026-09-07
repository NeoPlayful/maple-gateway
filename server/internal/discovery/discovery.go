// Package discovery 提供 Container Manager / Node Agent 的状态上报 Internal API。
// 独立于 admin Bearer 认证，使用 MAPLE_INTERNAL_TOKEN；不对公网开放。
package discovery

import (
	"os"
	"strings"

	"github.com/NeoPlayful/maple-gateway/server/pkg"
	"github.com/gofiber/fiber/v3"
)

// tokenEnv 是 Internal API 令牌的环境变量名。
const tokenEnv = "MAPLE_INTERNAL_TOKEN"

// token 读取一次并缓存（进程内配置不变）。
var cachedToken = ""

func init() {
	cachedToken = os.Getenv(tokenEnv)
}

// Middleware 校验 Internal Token（Authorization: Bearer <token>）。
func Middleware() fiber.Handler {
	return func(c fiber.Ctx) error {
		if cachedToken == "" {
			return pkg.Err(c, pkg.ErrUnauthorized("internal token 未配置"))
		}
		h := c.Get("Authorization")
		if !strings.HasPrefix(h, "Bearer ") || !strings.EqualFold(strings.TrimPrefix(h, "Bearer "), cachedToken) {
			return pkg.Err(c, pkg.ErrUnauthorized("无效的 internal token"))
		}
		return c.Next()
	}
}
