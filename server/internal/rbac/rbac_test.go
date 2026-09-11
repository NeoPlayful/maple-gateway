package rbac

import "testing"

func TestAuthorize_ReadsAllowedForAll(t *testing.T) {
	// 只读（GET）对所有角色放行。
	for _, role := range []string{RoleSuperAdmin, RoleOperator, RoleViewer} {
		for _, p := range []string{
			"/api/admin/tenants",
			"/api/admin/tenants/123",
			"/api/admin/settings",
			"/api/admin/logs/audit",
			"/api/admin/dashboard/overview",
		} {
			ok, action, _ := authorize(role, "GET", p)
			if !ok || action != ActionRead {
				t.Fatalf("role=%s GET %s should be allowed (read)", role, p)
			}
		}
	}
}

func TestAuthorize_OperatorWrites(t *testing.T) {
	// operator 可写资源配置与发布模块。
	for _, p := range []string{
		"/api/admin/tenants",
		"/api/admin/tenants/123/enable",
		"/api/admin/domains",
		"/api/admin/services/9",
		"/api/admin/instances",
		"/api/admin/nodes/5",
		"/api/admin/deployments",
		"/api/admin/deployments/1/versions",
		"/api/admin/versions/2",
		"/api/admin/canary/3/start",
		"/api/admin/blue-green",
		"/api/admin/rate-limits",
		"/api/admin/traffic",
	} {
		ok, _, _ := authorize(RoleOperator, "POST", p)
		if !ok {
			t.Fatalf("operator should write %s", p)
		}
	}
}

func TestAuthorize_OperatorDeniedOnGovernanceWrites(t *testing.T) {
	// operator 不能写治理/账号模块。
	for _, p := range []string{
		"/api/admin/settings/logging",
		"/api/admin/settings",
		"/api/admin/users",
		"/api/admin/cache/rebuild",
	} {
		ok, _, _ := authorize(RoleOperator, "PATCH", p)
		if ok {
			t.Fatalf("operator should NOT write %s", p)
		}
		ok, _, _ = authorize(RoleOperator, "POST", p)
		if ok {
			t.Fatalf("operator should NOT write %s", p)
		}
	}
}

func TestAuthorize_ViewerDeniedOnAllWrites(t *testing.T) {
	// viewer 任何写都拒绝。
	for _, p := range []string{
		"/api/admin/tenants",
		"/api/admin/deployments",
		"/api/admin/canary/1/start",
		"/api/admin/settings/logging",
		"/api/admin/rate-limits",
	} {
		for _, method := range []string{"POST", "PATCH", "DELETE"} {
			if ok, _, _ := authorize(RoleViewer, method, p); ok {
				t.Fatalf("viewer should NOT %s %s", method, p)
			}
		}
	}
}

func TestAuthorize_SuperAdminEverything(t *testing.T) {
	for _, p := range []string{
		"/api/admin/settings/logging",
		"/api/admin/users",
		"/api/admin/canary/1/rollback",
		"/api/admin/cache/rebuild",
		"/api/admin/tenants",
		"/api/admin/logs/audit",
	} {
		if ok, _, _ := authorize(RoleSuperAdmin, "POST", p); !ok {
			t.Fatalf("super_admin should write %s", p)
		}
		if ok, _, _ := authorize(RoleSuperAdmin, "GET", p); !ok {
			t.Fatalf("super_admin should read %s", p)
		}
	}
}

func TestAuthorize_SelfPaths(t *testing.T) {
	// 改自己密码/登出：任意已登录角色可执行。
	for _, role := range []string{RoleSuperAdmin, RoleOperator, RoleViewer} {
		for _, p := range []string{
			"/api/admin/auth/change-password",
			"/api/admin/auth/logout",
		} {
			if ok, _, _ := authorize(role, "POST", p); !ok {
				t.Fatalf("role=%s should self-write %s", role, p)
			}
		}
	}
}

func TestAuthorize_UnknownRoleSafe(t *testing.T) {
	// 未知角色：读放行、写拒绝。
	if ok, _, _ := authorize("unknown", "GET", "/api/admin/tenants"); !ok {
		t.Fatal("unknown role should read")
	}
	if ok, _, _ := authorize("unknown", "DELETE", "/api/admin/tenants/1"); ok {
		t.Fatal("unknown role should NOT write")
	}
}

func TestModuleOf(t *testing.T) {
	cases := map[string]string{
		"/api/admin/tenants":          "tenants",
		"/api/admin/tenants/1/enable": "tenants",
		"/api/admin/settings":         "settings",
		"/api/admin":                  "root",
	}
	for in, want := range cases {
		if got := moduleOf(in); got != want {
			t.Fatalf("moduleOf(%s)=%s want %s", in, got, want)
		}
	}
}
