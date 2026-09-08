package pkg

import (
	"errors"

	"github.com/gofiber/fiber/v3"
	"go.uber.org/zap"
)

// Response 统一响应结构。
type Response struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
	Meta    any    `json:"meta,omitempty"`
}

// OK 写成功响应。
func OK(c fiber.Ctx, data any) error {
	return c.JSON(Response{Code: "OK", Message: "ok", Data: data})
}

// OKMeta 写带分页等元信息的成功响应。
func OKMeta(c fiber.Ctx, data, meta any) error {
	return c.JSON(Response{Code: "OK", Message: "ok", Data: data, Meta: meta})
}

// Err 写错误响应。优先识别 *AppError，未知错误按系统错误处理。
func Err(c fiber.Ctx, err error) error {
	var ae *AppError
	if errors.As(err, &ae) {
		return c.Status(ae.Status).JSON(Response{Code: ae.Code, Message: ae.Message})
	}
	// debug 级：默认 info 不输出，排查未知 500 时把 log.level 调成 debug 即可看到真实错误。
	Log().Debug("unhandled error returned as 500", zap.Error(err))
	return c.Status(500).JSON(Response{Code: CodeSystemError, Message: "系统异常"})
}
