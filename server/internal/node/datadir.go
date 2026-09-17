package node

import (
	"path/filepath"
	"strings"

	"github.com/NeoPlayful/maple-gateway/server/pkg"
)

// normalizeDataDir 归一化数据根目录：去除首尾空白与结尾分隔符。
// 返回值为空串即表示未配置数据目录。
func normalizeDataDir(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	// 统一分隔符并去掉结尾多余的 "/"（根 "/" 本身除外）。
	s = filepath.ToSlash(s)
	for len(s) > 1 && strings.HasSuffix(s, "/") {
		s = s[:len(s)-1]
	}
	return s
}

// ValidateDataDir 校验数据根目录：必须是绝对路径且不含上跳段（".."）。
//
// 该目录是容器绑定挂载的宿主根，其下按 <租户>/<模板>/<项目> 分目录。管理员可为空
// 表示不配置；一旦配置则必须是干净的绝对路径，否则后续拼接出的宿主路径不可信。
func ValidateDataDir(s string) error {
	s = normalizeDataDir(s)
	if s == "" {
		return nil
	}
	if !strings.HasPrefix(s, "/") && !isWindowsAbs(s) {
		return pkg.ErrValidation("数据目录必须是绝对路径")
	}
	for _, seg := range strings.Split(s, "/") {
		if seg == ".." {
			return pkg.ErrValidation("数据目录不能包含 .. 上跳段")
		}
	}
	return nil
}

// isWindowsAbs 判断是否 Windows 盘符绝对路径（如 C:/data）。
func isWindowsAbs(s string) bool {
	if len(s) < 3 {
		return false
	}
	return ((s[0] >= 'a' && s[0] <= 'z') || (s[0] >= 'A' && s[0] <= 'Z')) &&
		s[1] == ':' && s[2] == '/'
}
