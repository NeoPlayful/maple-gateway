package rbac

import (
	"github.com/NeoPlayful/maple-gateway/server/internal/auth"
	"github.com/NeoPlayful/maple-gateway/server/pkg"
	"github.com/gofiber/fiber/v3"
)

// DenyAuditer 记录一次越权尝试（越权写 audit_logs）。
// 由装配层注入实现（依赖 ent），避免 rbac 直接耦合 ORM。
type DenyAuditer interface {
	// AuditDeny 在授权拒绝时被调用；adminID 为空表示未识别到管理员。
	AuditDeny(adminID, path, action, module string)
}

// Middleware 在 auth.Middleware 之后执行：按当前管理员 role 判定 method+path。
// 拒绝时返回 403 并同步触发越权审计（审计失败不影响主链路响应）。
func Middleware(a DenyAuditer) fiber.Handler {
	return func(c fiber.Ctx) error {
		role := auth.Role(c)
		allowed, action, module := authorize(role, c.Method(), c.Path())
		if allowed {
			return c.Next()
		}
		if a != nil {
			a.AuditDeny(auth.AdminID(c), c.Path(), action, module)
		}
		return pkg.Err(c, pkg.ErrForbidden("无权限执行该操作（需要更高角色）"))
	}
}
