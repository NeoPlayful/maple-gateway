package auth

import (
	"github.com/NeoPlayful/maple-gateway/server/pkg"
	"github.com/gofiber/fiber/v3"
)

// Handler 暴露认证相关路由处理器。
type Handler struct {
	svc *Service
}

// NewHandler 构造。
func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

// Login POST /api/auth/login
func (h *Handler) Login(c fiber.Ctx) error {
	var in LoginInput
	if err := c.Bind().Body(&in); err != nil {
		return pkg.Err(c, pkg.ErrValidation("请求体格式错误"))
	}
	if err := pkg.ValidateStruct(in); err != nil {
		return pkg.Err(c, err)
	}
	res, err := h.svc.Login(c.Context(), in)
	if err != nil {
		return pkg.Err(c, err)
	}
	return pkg.OK(c, res)
}

// Me GET /api/auth/me
func (h *Handler) Me(c fiber.Ctx) error {
	info, err := h.svc.AdminByID(c.Context(), AdminID(c))
	if err != nil {
		return pkg.Err(c, err)
	}
	return pkg.OK(c, info)
}

// Logout POST /api/auth/logout
func (h *Handler) Logout(c fiber.Ctx) error {
	// JWT 无状态，Phase 1 不维护黑名单；返回成功由前端清除本地令牌。
	return pkg.OK(c, map[string]any{"logged_out": true})
}
