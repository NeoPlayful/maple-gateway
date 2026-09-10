package gateway

import "net/http"

// ChallengeResponder 应答 ACME http-01 挑战：按请求 path 返回 keyAuthorization。
// 由 certificate/acme.ChallengeStore 实现；仅在数据平面启用时注入。
type ChallengeResponder interface {
	// RespondPath 命中挑战路径返回 (keyAuthorization, true)；否则 ("", false)，
	// 调用方应回落常规路由（避免误伤真实业务路径）。
	RespondPath(path string) (string, bool)
}

// challengeHandler 在代理之前短路应答 ACME http-01 挑战（RFC 8555 §8.3）。
// 命中即 200 + text/plain 返回 keyAuthorization；未命中原样透传给 next。
// 纯内存查找，不访问 DB，也不进入 Host 路由解析。
type challengeHandler struct {
	next http.Handler
	cr   ChallengeResponder
}

func (h challengeHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if h.cr != nil {
		if body, ok := h.cr.RespondPath(r.URL.Path); ok {
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(body))
			return
		}
	}
	h.next.ServeHTTP(w, r)
}
