package proxy

import (
	"context"

	"github.com/NeoPlayful/maple-gateway/server/internal/router"
)

type targetKey struct{}

// withTarget 把解析出的转发目标写入请求 context。
func withTarget(ctx context.Context, t *router.Target) context.Context {
	return context.WithValue(ctx, targetKey{}, t)
}

// targetFromContext 读取转发目标；未设置时返回 nil。
func targetFromContext(ctx context.Context) *router.Target {
	t, _ := ctx.Value(targetKey{}).(*router.Target)
	return t
}
