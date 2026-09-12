// Package rbac 提供 Management API 的角色访问控制（RBAC）。
//
// 角色：super_admin（全量+账号管理）/ operator（资源配置与发布）/ viewer（只读）。
// 判定纯函数（authorize）与 Fiber 中间件分离，便于单测。
package rbac

import (
	"strings"
)

// 角色（与 auth 包一致；此处独立常量避免跨包耦合由字符串桥接）。
const (
	RoleSuperAdmin = "super_admin"
	RoleOperator   = "operator"
	RoleViewer     = "viewer"
)

// 动作：由 HTTP method 推导。GET/HEAD=read，其余 write/operate。
const (
	ActionRead  = "read"
	ActionWrite = "write"
)

// operatorWritable 是 operator 角色可写的资源模块前缀（资源配置与发布）。
// 未列出的模块写操作（治理/账号/设置/审计查看类）仅 super_admin 可写。
var operatorWritable = []string{
	"/api/admin/tenants",
	"/api/admin/domains",
	"/api/admin/certificates",
	"/api/admin/services",
	"/api/admin/instances",
	"/api/admin/nodes",
	"/api/admin/deployments",
	"/api/admin/versions",
	"/api/admin/canary",
	"/api/admin/blue-green",
	"/api/admin/rate-limits",
	"/api/admin/traffic",
	"/api/admin/cm", // CM 运行时运维代理：实例 start/stop/restart
}

// selfWritePaths 是任何已登录管理员都可执行的写操作（作用于自身账号）。
var selfWritePaths = []string{
	"/api/admin/auth/change-password",
	"/api/admin/auth/logout",
}

// authorize 判定 role 对 method+path 是否允许。
// 返回 (allowed, action, module)。module 供审计记录被拒的资源。
// 判定规则：
//   - super_admin：全部放行；
//   - GET/HEAD：所有角色放行（只读基线）；
//   - 非 GET：operator 仅对 operatorWritable 模块放行；viewer 一律拒；
//   - selfWritePaths：任何角色放行（改自己密码/登出）。
func authorize(role, method, path string) (bool, string, string) {
	action := ActionWrite
	if method == "GET" || method == "HEAD" {
		action = ActionRead
	}
	module := moduleOf(path)

	// 自操作路径豁免（先于写权限判定）。
	if action == ActionWrite {
		for _, p := range selfWritePaths {
			if strings.HasPrefix(path, p) {
				return true, action, module
			}
		}
	}

	switch role {
	case RoleSuperAdmin:
		return true, action, module
	case RoleViewer:
		// viewer 只读。
		return action == ActionRead, action, module
	case RoleOperator:
		if action == ActionRead {
			return true, action, module
		}
		for _, p := range operatorWritable {
			if strings.HasPrefix(path, p) {
				return true, action, module
			}
		}
		return false, action, module
	default:
		// 未知角色一律拒绝写；读按只读放行（未知角色安全降级）。
		return action == ActionRead, action, module
	}
}

// moduleOf 取路径首资源段（/api/admin/tenants/:id → tenants）。
func moduleOf(path string) string {
	p := strings.TrimPrefix(path, "/api/admin/")
	if i := strings.IndexByte(p, '/'); i >= 0 {
		p = p[:i]
	}
	if p == "" {
		return "root"
	}
	return p
}
