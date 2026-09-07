// Package pkg 提供网关公共基础能力。
package pkg

import (
	"errors"
	"net/http"
)

// 业务错误码（与 API 文档统一响应结构对应）。
const (
	CodeUnauthorized    = "UNAUTHORIZED"
	CodeForbidden       = "FORBIDDEN"
	CodeValidationError = "VALIDATION_ERROR"
	CodeNotFound        = "NOT_FOUND"
	CodeConflict        = "CONFLICT"
	CodeRateLimited     = "RATE_LIMITED"
	CodeSystemError     = "SYSTEM_ERROR"
)

// AppError 是携带业务错误码的应用错误。
type AppError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Status  int    `json:"-"`
}

func (e *AppError) Error() string { return e.Message }

// NewAppError 构造应用错误。
func NewAppError(status int, code, message string) *AppError {
	return &AppError{Code: code, Message: message, Status: status}
}

// 便捷构造。
func ErrUnauthorized(msg string) *AppError {
	if msg == "" {
		msg = "未授权"
	}
	return NewAppError(http.StatusUnauthorized, CodeUnauthorized, msg)
}

func ErrForbidden(msg string) *AppError {
	if msg == "" {
		msg = "无权限"
	}
	return NewAppError(http.StatusForbidden, CodeForbidden, msg)
}

func ErrValidation(msg string) *AppError {
	if msg == "" {
		msg = "参数校验失败"
	}
	return NewAppError(http.StatusBadRequest, CodeValidationError, msg)
}

func ErrNotFound(msg string) *AppError {
	if msg == "" {
		msg = "资源不存在"
	}
	return NewAppError(http.StatusNotFound, CodeNotFound, msg)
}

func ErrConflict(msg string) *AppError {
	if msg == "" {
		msg = "资源冲突"
	}
	return NewAppError(http.StatusConflict, CodeConflict, msg)
}

func ErrSystem(msg string) *AppError {
	if msg == "" {
		msg = "系统异常"
	}
	return NewAppError(http.StatusInternalServerError, CodeSystemError, msg)
}

// ErrCode 从任意 error 提取业务错误码；非 AppError 视为系统错误。
func ErrCode(err error) string {
	var ae *AppError
	if errors.As(err, &ae) {
		return ae.Code
	}
	return CodeSystemError
}
