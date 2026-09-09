package proxy

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"sync/atomic"
	"time"
)

// RequestIDHeader 是贯穿数据平面与上游的请求关联头。
const RequestIDHeader = "X-Request-Id"

type requestIDKey struct{}

// requestIDFromContext 读取请求 ID；未设置返回空串。
func requestIDFromContext(ctx context.Context) string {
	v, _ := ctx.Value(requestIDKey{}).(string)
	return v
}

// ensureRequestID 为请求生成/透传请求 ID：
//   - 客户端已带合法 X-Request-Id → 透传（跨网关/上下游保持一致）；
//   - 无 → 生成 16 字节随机 hex（32 位）。
//
// 请求头写回（reverse proxy 克隆时带出到上游），context 同步写入供日志/trace 关联。
func ensureRequestID(r *http.Request) (*http.Request, string) {
	if rid := r.Header.Get(RequestIDHeader); rid != "" {
		return r.WithContext(withRequestID(r.Context(), rid)), rid
	}
	rid := newRequestID()
	r.Header.Set(RequestIDHeader, rid)
	return r.WithContext(withRequestID(r.Context(), rid)), rid
}

func withRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, requestIDKey{}, id)
}

var randFallbackSeq atomic.Uint64

// newRequestID 生成 16 字节随机 hex（32 位）。crypto/rand 异常时退化时间+计数。
func newRequestID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err == nil {
		return hex.EncodeToString(b[:])
	}
	return time.Now().Format("150405.000000000") + "-" + itoa(int(randFallbackSeq.Add(1)))
}
