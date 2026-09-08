package settings

import (
	"context"
	"encoding/json"

	"github.com/NeoPlayful/maple-gateway/server/pkg"
)

// SyncLogLevel 按 settings 里 logging.debug 的值同步全局日志输出级别。
// 在日志分区保存后与进程启动加载 settings 时调用，使 debug 开关持久生效。
func SyncLogLevel(repo *Repository) {
	debug := repo.GetBool(SectionLogging, "debug", false)
	pkg.SetLogLevel(debug)
}

// EnsureDefaultDebug 幂等确保 logging.debug 存在默认行（false）。
// 让设置页日志分区无论何时都能渲染出 debug 开关（首次打开也可见）。
func EnsureDefaultDebug(ctx context.Context, repo *Repository) error {
	if _, ok := repo.Get(SectionLogging, "debug"); ok {
		return nil
	}
	_, err := repo.Upsert(ctx, SectionLogging, "debug", json.RawMessage("false"))
	return err
}
