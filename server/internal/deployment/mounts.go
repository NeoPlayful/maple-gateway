package deployment

import (
	"fmt"
	"strings"

	"github.com/NeoPlayful/maple-gateway/server/pkg"
)

// ValidateMounts 校验绑定挂载规格。这是越界防线的第一道（第二道在 Node Agent）：
// 拒绝绝对的、含上跳段的、以及重复的 path/target，避免把宿主任意目录挂进容器。
func ValidateMounts(mounts []Mount) error {
	if len(mounts) == 0 {
		return nil
	}
	seenPath := map[string]bool{}
	seenTarget := map[string]bool{}
	for i, m := range mounts {
		p := strings.TrimSpace(strings.ReplaceAll(m.Path, "\\", "/"))
		if err := pkg.ValidateRelSubpath("数据根子路径", p); err != nil {
			return pkg.ErrValidation(fmt.Sprintf("挂载项 %d：%s", i+1, err.Error()))
		}
		target := strings.TrimSpace(m.Target)
		if target == "" || !strings.HasPrefix(target, "/") {
			return pkg.ErrValidation(fmt.Sprintf("挂载项 %d：容器内路径必须是绝对路径", i+1))
		}
		if seenPath[p] {
			return pkg.ErrValidation(fmt.Sprintf("挂载项 %d：数据根子路径 %q 重复", i+1, p))
		}
		if seenTarget[target] {
			return pkg.ErrValidation(fmt.Sprintf("挂载项 %d：容器内路径 %q 重复", i+1, target))
		}
		seenPath[p] = true
		seenTarget[target] = true
	}
	return nil
}
