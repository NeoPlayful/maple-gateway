// Package tracex 提供数据平面 OpenTelemetry 追踪的薄封装。
//
// 职责：按配置初始化全局 TracerProvider（stdout 导出 + 采样），暴露 Tracer/Span
// 供 proxy 埋点，避免数据平面核心直接 import otel。span 记录 request_id 属性，
// 与访问/错误日志关联。
package tracex

import (
	"context"
	"fmt"
	"net/http"
	"os"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

// Config 是追踪初始化配置。
type Config struct {
	Enabled     bool    // false 时 Setup 返回 nil（不初始化 TracerProvider）
	SampleRatio float64 // 采样率 0-1
	ServiceName string  // resource service.name
}

// Tracer 是 proxy 埋点用的追踪器句柄。
type Tracer interface {
	// StartServer 开启 server-kind span（数据平面根 span，host+method）。
	StartServer(ctx context.Context, name string) (context.Context, *Span)
	// Start 开启默认（internal）子 span（如路由解析）。
	Start(ctx context.Context, name string) (context.Context, *Span)
}

// Span 是 otel trace.Span 的最小包装，语义化完成 HTTP 请求/内部步骤。
type Span struct {
	s trace.Span
}

// SetAttributes 追加 span 属性。
func (s *Span) SetAttributes(kv ...attribute.KeyValue) {
	if s == nil || s.s == nil {
		return
	}
	s.s.SetAttributes(kv...)
}

// SetRequestID 记录 request_id 属性，供日志/trace 关联。
func (s *Span) SetRequestID(rid string) {
	s.SetAttributes(RequestIDAttr(rid))
}

// SetTargetPath 记录 http.target 属性。
func (s *Span) SetTargetPath(path string) {
	if s == nil || s.s == nil || path == "" {
		return
	}
	s.s.SetAttributes(attribute.String("http.target", path))
}

// FinishHTTP 以 HTTP 结果结束 span：记录 http.status_code；err 或 5xx 置 error。
func (s *Span) FinishHTTP(code int, err error) {
	if s == nil || s.s == nil {
		return
	}
	s.s.SetAttributes(attribute.Int("http.status_code", code))
	if err != nil {
		s.s.RecordError(err)
		s.s.SetStatus(codes.Error, err.Error())
	} else if code >= 500 {
		s.s.SetStatus(codes.Error, http.StatusText(code))
	}
	s.s.End()
}

// Finish 结束内部步骤 span（无 HTTP 状态）；err 非空则记 error。
func (s *Span) Finish(err error) {
	if s == nil || s.s == nil {
		return
	}
	if err != nil {
		s.s.RecordError(err)
		s.s.SetStatus(codes.Error, err.Error())
	}
	s.s.End()
}

// tracer 包装全局 trace.Tracer。
type tracer struct {
	t trace.Tracer
}

func (t *tracer) StartServer(ctx context.Context, name string) (context.Context, *Span) {
	return t.start(ctx, name, trace.WithSpanKind(trace.SpanKindServer))
}

func (t *tracer) Start(ctx context.Context, name string) (context.Context, *Span) {
	return t.start(ctx, name, trace.WithSpanKind(trace.SpanKindInternal))
}

func (t *tracer) start(ctx context.Context, name string, opts ...trace.SpanStartOption) (context.Context, *Span) {
	ctx, sp := t.t.Start(ctx, name, opts...)
	return ctx, &Span{s: sp}
}

// NewTracer 返回可直接埋点的 Tracer；nil 输入返回 nil（禁用追踪）。
func NewTracer(t trace.Tracer) Tracer {
	if t == nil {
		return nil
	}
	return &tracer{t: t}
}

// RequestIDAttr 构造 request_id 关联属性。
func RequestIDAttr(rid string) attribute.KeyValue {
	return attribute.String("request_id", rid)
}

// Setup 按配置初始化全局 TracerProvider，返回（Tracer, 关闭函数, error）。
// Enabled=false 时不做任何替换，返回 (nil, no-op, nil)——调用方判空跳过埋点。
func Setup(cfg Config) (Tracer, func(), error) {
	if !cfg.Enabled {
		return nil, func() {}, nil
	}
	ratio := cfg.SampleRatio
	if ratio <= 0 {
		ratio = 0
	}
	if ratio > 1 {
		ratio = 1
	}

	exp, err := stdouttrace.New(stdouttrace.WithWriter(os.Stdout))
	if err != nil {
		return nil, nil, fmt.Errorf("tracex: create exporter: %w", err)
	}

	name := cfg.ServiceName
	if name == "" {
		name = "maple-gateway"
	}
	res := resource.NewSchemaless(attribute.String("service.name", name))

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exp),
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sdktrace.ParentBased(sdktrace.TraceIDRatioBased(ratio))),
	)
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.TraceContext{})

	return NewTracer(tp.Tracer("maple-gateway")), func() {
		_ = tp.Shutdown(context.Background())
	}, nil
}
