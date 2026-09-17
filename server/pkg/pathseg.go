package pkg

import (
	"fmt"
	"path"
	"strings"
)

// segAllowedChars 是路径段允许的字符集：ASCII 字母、数字、下划线、连字符、点。
// 项目标识/模板标识/租户标识作为数据目录的路径段，必须限制在此集合内，
// 否则可能引入分隔符或上跳段，破坏租户隔离。
func segAllowed(r rune) bool {
	switch {
	case r >= 'a' && r <= 'z':
		return true
	case r >= 'A' && r <= 'Z':
		return true
	case r >= '0' && r <= '9':
		return true
	case r == '_' || r == '-' || r == '.':
		return true
	}
	return false
}

// ValidatePathSegment 校验一个数据目录路径段（单段，不含分隔符）。
// 必须非空、长度受限、仅含安全字符，且不得是 "." / ".."（防上跳）。
func ValidatePathSegment(field, s string) error {
	if s == "" {
		return ErrValidation(fmt.Sprintf("%s 不能为空", field))
	}
	if len(s) > 64 {
		return ErrValidation(fmt.Sprintf("%s 长度不能超过 64", field))
	}
	if s == "." || s == ".." {
		return ErrValidation(fmt.Sprintf("%s 不能为 %q", field, s))
	}
	for _, r := range s {
		if !segAllowed(r) {
			return ErrValidation(fmt.Sprintf("%s 只能包含字母、数字、下划线、连字符与点", field))
		}
	}
	return nil
}

// ValidateRelSubpath 校验相对数据根的多段子路径：非空、非绝对、无上跳/冗余段，
// 且每段均通过 ValidatePathSegment。
func ValidateRelSubpath(field, p string) error {
	p = strings.TrimSpace(strings.ReplaceAll(p, "\\", "/"))
	if p == "" {
		return ErrValidation(fmt.Sprintf("%s 不能为空", field))
	}
	if strings.HasPrefix(p, "/") || isWindowsAbsPath(p) {
		return ErrValidation(fmt.Sprintf("%s 必须是相对路径", field))
	}
	if p != path.Clean(p) {
		return ErrValidation(fmt.Sprintf("%s 含冗余或上跳段", field))
	}
	for _, seg := range strings.Split(p, "/") {
		if err := ValidatePathSegment(field, seg); err != nil {
			return err
		}
	}
	return nil
}

// isWindowsAbsPath 判断是否 Windows 盘符绝对路径（如 C:/data）。
func isWindowsAbsPath(s string) bool {
	if len(s) < 3 {
		return false
	}
	return ((s[0] >= 'a' && s[0] <= 'z') || (s[0] >= 'A' && s[0] <= 'Z')) &&
		s[1] == ':' && (s[2] == '/' || s[2] == '\\')
}
