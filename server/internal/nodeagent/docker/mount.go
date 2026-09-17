package docker

import (
	"fmt"
	"path"
	"strings"
)

// normalizeRoot 归一化数据根目录：转正斜杠、去除结尾分隔符。
// 返回空串表示未配置数据根。
func normalizeRoot(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	s = toSlash(s)
	for len(s) > 1 && strings.HasSuffix(s, "/") {
		s = s[:len(s)-1]
	}
	return s
}

// buildBinds 把挂载规格解析为宿主机绝对路径，并逐项做越界校验后转成 Docker 绑定串。
//
// 控制面只下发相对于数据根的相对路径（Mount.Path），由本节点自行拼出宿主绝对路径：
// 数据根因此始终只存在于节点本地配置，控制面无需、也无法指定任意的宿主目录。
// 相对路径中的上跳段（".."）在此被拒绝，容器无法把数据根之外的目录挂进容器。
func (c *Client) buildBinds(mounts []Mount) ([]string, error) {
	if len(mounts) == 0 {
		return nil, nil
	}
	if c.dataRoot == "" {
		return nil, fmt.Errorf("节点未配置数据根（data_root），无法创建带挂载的容器")
	}
	binds := make([]string, 0, len(mounts))
	for _, m := range mounts {
		target := strings.TrimSpace(m.Target)
		if target == "" || !strings.HasPrefix(target, "/") {
			return nil, fmt.Errorf("mount: target %q 必须是容器内绝对路径", m.Target)
		}
		sub, err := cleanSubdir(m.Path)
		if err != nil {
			return nil, err
		}
		source := path.Join(c.dataRoot, sub)
		if !withinRoot(c.dataRoot, source) {
			return nil, fmt.Errorf("mount: 宿主路径 %q 越出数据根 %q", source, c.dataRoot)
		}
		bind := source + ":" + target
		if m.ReadOnly {
			bind += ":ro"
		}
		binds = append(binds, bind)
	}
	return binds, nil
}

// cleanSubdir 校验并归一化一个相对子路径：必须非空、非绝对、不含上跳段与空段。
func cleanSubdir(p string) (string, error) {
	p = strings.TrimSpace(toSlash(p))
	if p == "" {
		return "", fmt.Errorf("mount: path 不能为空")
	}
	if strings.HasPrefix(p, "/") || isWindowsAbs(p) {
		return "", fmt.Errorf("mount: path %q 必须是相对数据根的相对路径", p)
	}
	if p != path.Clean(p) {
		return "", fmt.Errorf("mount: path %q 含冗余或上跳段", p)
	}
	for _, seg := range strings.Split(p, "/") {
		if seg == ".." || seg == "." || seg == "" {
			return "", fmt.Errorf("mount: path %q 含非法路径段", p)
		}
	}
	return p, nil
}

// withinRoot 判断 child 是否等于 root 或位于 root 之下（均已归一化为正斜杠）。
// 按路径段比较，避免 "/data/maple-x" 被误判为落在 "/data/maple" 之下。
func withinRoot(root, child string) bool {
	root = normalizeRoot(root)
	child = path.Clean(toSlash(child))
	if root == "" {
		return false
	}
	if child == root {
		return true
	}
	return strings.HasPrefix(child, root+"/")
}

// isWindowsAbs 判断是否 Windows 盘符绝对路径（如 C:/data）。
func isWindowsAbs(s string) bool {
	if len(s) < 3 {
		return false
	}
	return ((s[0] >= 'a' && s[0] <= 'z') || (s[0] >= 'A' && s[0] <= 'Z')) &&
		s[1] == ':' && (s[2] == '/' || s[2] == '\\')
}

// toSlash 统一分隔符为正斜杠（兼容控制面下发的 Windows 风格路径）。
func toSlash(s string) string { return strings.ReplaceAll(s, "\\", "/") }
