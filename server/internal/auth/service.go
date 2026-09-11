// Package auth 提供 Management API 的管理员认证与授权。
package auth

import (
	"context"
	"errors"
	"os"
	"time"

	"github.com/NeoPlayful/maple-gateway/server/ent"
	entuser "github.com/NeoPlayful/maple-gateway/server/ent/user"
	"github.com/NeoPlayful/maple-gateway/server/pkg"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
)

// 管理员角色（RBAC）。与 admins.role 列、internal/rbac 授权矩阵一致。
const (
	RoleSuperAdmin = "super_admin"
	RoleOperator   = "operator"
	RoleViewer     = "viewer"
)

// ValidRole 判断角色是否合法。
func ValidRole(r string) bool {
	switch r {
	case RoleSuperAdmin, RoleOperator, RoleViewer:
		return true
	}
	return false
}

// Claims 是 JWT 载荷。
type Claims struct {
	AdminID string `json:"aid"`
	Email   string `json:"email"`
	Role    string `json:"role"`
	jwt.RegisteredClaims
}

// Service 封装认证逻辑。
type Service struct {
	ent    *ent.Client
	secret []byte
	ttl    time.Duration
}

// NewService 构造认证服务。
func NewService(client *ent.Client, secret []byte, ttl time.Duration) *Service {
	return &Service{ent: client, secret: secret, ttl: ttl}
}

// Secret 从环境变量读取，缺省用开发默认（生产必须注入 MAPLE_JWT_SECRET）。
func Secret() []byte {
	if v := os.Getenv("MAPLE_JWT_SECRET"); v != "" {
		return []byte(v)
	}
	return []byte("dev-secret-change-me")
}

// LoginInput 登录入参。
type LoginInput struct {
	Email    string `json:"email" validate:"required,email"`
	Password string `json:"password" validate:"required"`
}

// LoginResult 登录结果。
type LoginResult struct {
	Token string    `json:"token"`
	Admin AdminInfo `json:"admin"`
}

// AdminInfo 返回给前端的脱敏管理员信息。
type AdminInfo struct {
	ID    string `json:"id"`
	Email string `json:"email"`
	Name  string `json:"name"`
	Role  string `json:"role"`
}

// Login 校验邮箱密码，签发 JWT。失败统一返回"邮箱或密码错误"避免账号枚举。
func (s *Service) Login(ctx context.Context, in LoginInput) (*LoginResult, error) {
	a, err := s.ent.User.Query().
		Where(entuser.EmailEQ(in.Email)).
		Only(ctx)
	if ent.IsNotFound(err) {
		return nil, pkg.ErrUnauthorized("邮箱或密码错误")
	}
	if err != nil {
		return nil, pkg.ErrSystem("查询管理员失败")
	}
	if a.Status != "active" {
		return nil, pkg.ErrUnauthorized("管理员已被禁用")
	}
	if err := bcrypt.CompareHashAndPassword([]byte(a.PasswordHash), []byte(in.Password)); err != nil {
		return nil, pkg.ErrUnauthorized("邮箱或密码错误")
	}

	name := a.Name
	if name == nil {
		name = stringPtr("")
	}
	role := a.Role
	if role == "" {
		role = RoleSuperAdmin // 存量行无 role（未迁移前）按超管兜底，不阻断登录
	}
	signed, err := s.signToken(a.ID.String(), a.Email, role)
	if err != nil {
		return nil, err
	}
	return &LoginResult{
		Token: signed,
		Admin: AdminInfo{ID: a.ID.String(), Email: a.Email, Name: *name, Role: role},
	}, nil
}

// signToken 为指定管理员签发新 JWT（带 role 声明）。
func (s *Service) signToken(adminID, email, role string) (string, error) {
	now := time.Now()
	claims := Claims{
		AdminID: adminID,
		Email:   email,
		Role:    role,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   adminID,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(s.ttl)),
		},
	}
	signed, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(s.secret)
	if err != nil {
		return "", pkg.ErrSystem("签发 Token 失败")
	}
	return signed, nil
}

// Refresh 校验旧 token 仍有效且管理员可用，返回新 token（无状态 JWT 的滑动续期）。
func (s *Service) Refresh(ctx context.Context, oldToken string) (*LoginResult, error) {
	claims, err := s.Parse(oldToken)
	if err != nil {
		return nil, err
	}
	info, err := s.AdminByID(ctx, claims.AdminID)
	if err != nil {
		return nil, err
	}
	token, err := s.signToken(claims.AdminID, info.Email, info.Role)
	if err != nil {
		return nil, err
	}
	return &LoginResult{Token: token, Admin: *info}, nil
}

// RefreshInput 刷新入参。
type RefreshInput struct {
	Token string `json:"token" validate:"required"`
}

// ChangePasswordInput 改密入参。
type ChangePasswordInput struct {
	OldPassword string `json:"old_password" validate:"required"`
	NewPassword string `json:"new_password" validate:"required,min=8"`
}

// ChangePassword 校验旧密码并更新为新密码。JWT 无状态，成功后前端重登即可。
func (s *Service) ChangePassword(ctx context.Context, adminID string, in ChangePasswordInput) error {
	uid, err := uuid.Parse(adminID)
	if err != nil {
		return pkg.ErrUnauthorized("管理员不存在")
	}
	a, err := s.ent.User.Get(ctx, uid)
	if ent.IsNotFound(err) {
		return pkg.ErrUnauthorized("管理员不存在")
	}
	if err != nil {
		return pkg.ErrSystem("查询管理员失败")
	}
	if err := bcrypt.CompareHashAndPassword([]byte(a.PasswordHash), []byte(in.OldPassword)); err != nil {
		return pkg.ErrUnauthorized("旧密码错误")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(in.NewPassword), bcrypt.DefaultCost)
	if err != nil {
		return pkg.ErrSystem("密码加密失败")
	}
	if _, err := s.ent.User.UpdateOneID(uid).
		SetPasswordHash(string(hash)).
		Save(ctx); err != nil {
		return pkg.ErrSystem("更新密码失败")
	}
	return nil
}

// AdminByID 查询管理员基础信息（me 接口）。
func (s *Service) AdminByID(ctx context.Context, id string) (*AdminInfo, error) {
	uid, err := uuid.Parse(id)
	if err != nil {
		return nil, pkg.ErrUnauthorized("管理员不存在")
	}
	a, err := s.ent.User.Get(ctx, uid)
	if ent.IsNotFound(err) {
		return nil, pkg.ErrUnauthorized("管理员不存在")
	}
	if err != nil {
		return nil, pkg.ErrSystem("查询管理员失败")
	}
	if a.Status != "active" {
		return nil, pkg.ErrForbidden("管理员已被禁用")
	}
	name := a.Name
	if name == nil {
		name = stringPtr("")
	}
	role := a.Role
	if role == "" {
		role = RoleSuperAdmin // 存量兜底
	}
	return &AdminInfo{ID: a.ID.String(), Email: a.Email, Name: *name, Role: role}, nil
}

func stringPtr(s string) *string { return &s }

// Parse 校验 token 并返回 Claims。
func (s *Service) Parse(tokenStr string) (*Claims, error) {
	claims := &Claims{}
	token, err := jwt.ParseWithClaims(tokenStr, claims, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, errors.New("unexpected signing method")
		}
		return s.secret, nil
	})
	if err != nil || !token.Valid {
		return nil, pkg.ErrUnauthorized("Token 无效或已过期")
	}
	return claims, nil
}
