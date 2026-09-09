package api

import (
	"time"

	"github.com/NeoPlayful/maple-gateway/server/ent"
	entadmin "github.com/NeoPlayful/maple-gateway/server/ent/admin"
	"github.com/NeoPlayful/maple-gateway/server/internal/auth"
	"github.com/NeoPlayful/maple-gateway/server/pkg"
	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
)

// adminHandler 暴露管理员账号管理（仅 super_admin 可用，经 RBAC 拦截）。
type adminHandler struct {
	ent *ent.Client
}

// adminRow 管理员列表/详情行。
type adminRow struct {
	ID        string    `json:"id"`
	Email     string    `json:"email"`
	Name      string    `json:"name"`
	Role      string    `json:"role"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
}

// listRow 由 ent Admin 转 adminRow。
func adminRowFrom(e *ent.Admin) adminRow {
	r := adminRow{
		ID:        e.ID.String(),
		Email:     e.Email,
		Role:      e.Role,
		Status:    e.Status,
		CreatedAt: e.CreatedAt,
	}
	if e.Name != nil {
		r.Name = *e.Name
	}
	return r
}

// List GET /api/admin/admins 列出管理员账号。
func (h *adminHandler) List(c fiber.Ctx) error {
	es, err := h.ent.Admin.Query().
		Order(entadmin.ByCreatedAt()).
		All(c.Context())
	if err != nil {
		return pkg.Err(c, err)
	}
	out := make([]adminRow, 0, len(es))
	for _, e := range es {
		out = append(out, adminRowFrom(e))
	}
	return pkg.OK(c, out)
}

// createAdminInput 创建管理员入参。
type createAdminInput struct {
	Email    string `json:"email" validate:"required,email"`
	Password string `json:"password" validate:"required,min=8"`
	Name     string `json:"name"`
	Role     string `json:"role"`
}

// Create POST /api/admin/admins 创建管理员（默认 operator；role 须合法）。
func (h *adminHandler) Create(c fiber.Ctx) error {
	var in createAdminInput
	if err := c.Bind().Body(&in); err != nil {
		return pkg.Err(c, pkg.ErrValidation("请求体格式错误"))
	}
	if err := pkg.ValidateStruct(in); err != nil {
		return pkg.Err(c, err)
	}
	role := in.Role
	if role == "" {
		role = auth.RoleOperator
	}
	if !auth.ValidRole(role) {
		return pkg.Err(c, pkg.ErrValidation("role 须为 super_admin / operator / viewer"))
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(in.Password), bcrypt.DefaultCost)
	if err != nil {
		return pkg.Err(c, pkg.ErrSystem("密码加密失败"))
	}
	e, err := h.ent.Admin.Create().
		SetEmail(in.Email).
		SetPasswordHash(string(hash)).
		SetRole(role).
		SetStatus("active").
		SetCreatedAt(time.Now()).
		SetUpdatedAt(time.Now()).
		SetNillableName(nilName(in.Name)).
		Save(c.Context())
	if err != nil {
		if ent.IsConstraintError(err) {
			return pkg.Err(c, pkg.ErrValidation("邮箱已存在"))
		}
		return pkg.Err(c, pkg.ErrSystem("创建管理员失败"))
	}
	return pkg.OK(c, adminRowFrom(e))
}

// setRoleInput 改角色入参。
type setRoleInput struct {
	Role string `json:"role" validate:"required"`
}

// SetRole PATCH /api/admin/admins/:id/role 改角色。
// 禁止把最后一个超管降级（防止锁死系统）。
func (h *adminHandler) SetRole(c fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return pkg.Err(c, pkg.ErrValidation("无效的 id"))
	}
	var in setRoleInput
	if err := c.Bind().Body(&in); err != nil {
		return pkg.Err(c, pkg.ErrValidation("请求体格式错误"))
	}
	if !auth.ValidRole(in.Role) {
		return pkg.Err(c, pkg.ErrValidation("role 须为 super_admin / operator / viewer"))
	}
	target, err := h.ent.Admin.Get(c.Context(), id)
	if err != nil {
		return pkg.Err(c, pkg.ErrValidation("管理员不存在"))
	}
	// 降级最后一名超管：先数超管数。
	if target.Role == auth.RoleSuperAdmin && in.Role != auth.RoleSuperAdmin {
		n, err := h.ent.Admin.Query().Where(entadmin.RoleEQ(auth.RoleSuperAdmin), entadmin.StatusEQ("active")).Count(c.Context())
		if err != nil {
			return pkg.Err(c, pkg.ErrSystem("查询管理员失败"))
		}
		if n <= 1 {
			return pkg.Err(c, pkg.ErrValidation("至少保留一名超管"))
		}
	}
	if _, err := h.ent.Admin.UpdateOneID(id).
		SetRole(in.Role).
		SetUpdatedAt(time.Now()).
		Save(c.Context()); err != nil {
		return pkg.Err(c, pkg.ErrSystem("更新角色失败"))
	}
	return pkg.OK(c, fiber.Map{"id": id.String(), "role": in.Role})
}

// toggleStatusInput 启/禁用入参。
type toggleStatusInput struct {
	Status string `json:"status" validate:"required,oneof=active disabled"`
}

// ToggleStatus PATCH /api/admin/admins/:id/status 启用/禁用账号。
// 禁止禁用自己（避免自锁）。
func (h *adminHandler) ToggleStatus(c fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return pkg.Err(c, pkg.ErrValidation("无效的 id"))
	}
	if id.String() == auth.AdminID(c) {
		return pkg.Err(c, pkg.ErrValidation("不能修改自己的状态"))
	}
	var in toggleStatusInput
	if err := c.Bind().Body(&in); err != nil {
		return pkg.Err(c, pkg.ErrValidation("请求体格式错误"))
	}
	if _, err := h.ent.Admin.UpdateOneID(id).
		SetStatus(in.Status).
		SetUpdatedAt(time.Now()).
		Save(c.Context()); err != nil {
		if ent.IsNotFound(err) {
			return pkg.Err(c, pkg.ErrValidation("管理员不存在"))
		}
		return pkg.Err(c, pkg.ErrSystem("更新状态失败"))
	}
	return pkg.OK(c, fiber.Map{"id": id.String(), "status": in.Status})
}

func nilName(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
