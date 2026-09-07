// Package auth 提供 Management API 的管理员认证与授权。
package auth

import (
	"context"
	"errors"
	"os"
	"time"

	"github.com/NeoPlayful/maple-gateway/server/pkg"
	"github.com/golang-jwt/jwt/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"
)

// Claims 是 JWT 载荷。
type Claims struct {
	AdminID string `json:"aid"`
	Email   string `json:"email"`
	jwt.RegisteredClaims
}

// Service 封装认证逻辑。
type Service struct {
	pool   *pgxpool.Pool
	secret []byte
	ttl    time.Duration
}

// NewService 构造认证服务。
func NewService(pool *pgxpool.Pool, secret []byte, ttl time.Duration) *Service {
	return &Service{pool: pool, secret: secret, ttl: ttl}
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
}

// Login 校验邮箱密码，签发 JWT。失败统一返回"邮箱或密码错误"避免账号枚举。
func (s *Service) Login(ctx context.Context, in LoginInput) (*LoginResult, error) {
	var (
		id, email, name, status, hash string
	)
	err := s.pool.QueryRow(ctx,
		`SELECT id::text, email, name, status, password_hash FROM admins WHERE email=$1`,
		in.Email).Scan(&id, &email, &name, &status, &hash)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, pkg.ErrUnauthorized("邮箱或密码错误")
	}
	if err != nil {
		return nil, pkg.ErrSystem("查询管理员失败")
	}
	if status != "active" {
		return nil, pkg.ErrUnauthorized("管理员已被禁用")
	}
	if err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(in.Password)); err != nil {
		return nil, pkg.ErrUnauthorized("邮箱或密码错误")
	}

	now := time.Now()
	claims := Claims{
		AdminID: id,
		Email:   email,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   id,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(s.ttl)),
		},
	}
	signed, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(s.secret)
	if err != nil {
		return nil, pkg.ErrSystem("签发 Token 失败")
	}

	return &LoginResult{
		Token: signed,
		Admin: AdminInfo{ID: id, Email: email, Name: name},
	}, nil
}

// AdminByID 查询管理员基础信息（me 接口）。
func (s *Service) AdminByID(ctx context.Context, id string) (*AdminInfo, error) {
	var info AdminInfo
	var status string
	err := s.pool.QueryRow(ctx,
		`SELECT id::text, email, name, status FROM admins WHERE id=$1`, id).
		Scan(&info.ID, &info.Email, &info.Name, &status)
	if err != nil {
		return nil, pkg.ErrUnauthorized("管理员不存在")
	}
	if status != "active" {
		return nil, pkg.ErrForbidden("管理员已被禁用")
	}
	return &info, nil
}

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
