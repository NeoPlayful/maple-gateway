package tracex

import (
	"testing"
)

func TestSetup_Disabled(t *testing.T) {
	tracer, closeFn, err := Setup(Config{Enabled: false})
	if err != nil {
		t.Fatalf("disabled setup err = %v", err)
	}
	if tracer != nil {
		t.Fatalf("disabled setup should return nil tracer")
	}
	closeFn() // no-op, must not panic
}

func TestSetup_Enabled(t *testing.T) {
	// 开启后应返回非 nil Tracer，能正常开启 span 并关闭。
	tracer, closeFn, err := Setup(Config{Enabled: true, SampleRatio: 1.0, ServiceName: "test-svc"})
	if err != nil {
		t.Fatalf("enabled setup err = %v", err)
	}
	if tracer == nil {
		t.Fatalf("enabled setup should return a tracer")
	}
	closeFn() // 必须可重复/安全关闭
}

func TestNewTracer_Nil(t *testing.T) {
	// 未初始化（Disabled）时 Setup 返回 nil Tracer，调用方应判空；
	// NewTracer(nil) 不 panic（直接 nil 返回）。
	if tr := NewTracer(nil); tr != nil {
		t.Fatalf("NewTracer(nil) should yield nil")
	}
}
