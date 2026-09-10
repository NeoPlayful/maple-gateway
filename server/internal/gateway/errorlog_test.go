package gateway

import (
	"testing"

	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

// TestZapErrorLogDropsSNISentinel: SNI 未命中哨兵错误（已由 onMiss 带 sni+地址记录）
// 不重复输出；其余错误按 Debug 记录并剥离标准库时间戳前缀。
func TestZapErrorLogDropsSNISentinel(t *testing.T) {
	core, logs := observer.New(zap.DebugLevel)
	w := newZapErrorLog(zap.New(core))

	// 标准库 log 前缀 + 哨兵错误 → 丢弃。
	_, _ = w.Write([]byte("2026/09/10 10:23:03 http: TLS handshake error from 127.0.0.1:51629: no certificate for server name\n"))
	if logs.Len() != 0 {
		t.Fatalf("SNI sentinel should be dropped, got %d entries", logs.Len())
	}

	// 其它握手错误 → 记一条 Debug，且不含时间戳前缀。
	_, _ = w.Write([]byte("2026/09/10 10:23:03 http: TLS handshake error from 127.0.0.1:51630: remote error: tls: bad certificate\n"))
	if logs.Len() != 1 {
		t.Fatalf("expected 1 entry for non-sentinel error, got %d", logs.Len())
	}
	e := logs.All()[0]
	if e.Level != zap.DebugLevel {
		t.Fatalf("expected Debug level, got %v", e.Level)
	}
	got := e.ContextMap()["err"]
	if s, _ := got.(string); len(s) < 14 || s[:14] != "http: TLS hand" {
		t.Fatalf("expected prefix-trimmed message, got %q", got)
	}
}
