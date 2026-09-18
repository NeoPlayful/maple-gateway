package pkg

import "strings"

// ShortID 把 UUID 去连字符后截取前 12 位，用作容器名与 Compose 项目名的后缀。
// 声明式容器名与 Compose 项目名共用它，避免两处各自截断导致长度不一致。
func ShortID(id string) string {
	s := strings.ReplaceAll(id, "-", "")
	if len(s) > 12 {
		return s[:12]
	}
	return s
}
