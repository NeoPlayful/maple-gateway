// Package router 提供多租户路由能力。
//
// 数据平面请求按 Host 解析出目标 upstream（Target）。S2 阶段由静态配置
// resolver 实现；S6 起替换为基于 Memory Route Cache 的动态实现。
package router

import (
	"context"
	"errors"
	"fmt"
)

// 路由层哨兵错误。S6 动态 resolver 返回更精细的 *pkg.AppError；
// S2 静态阶段用哨兵错误表达主要拒绝原因，由数据平面映射为 HTTP 状态码。
var (
	ErrNotFound        = errors.New("route: host not found")
	ErrDomainDisabled  = errors.New("route: domain disabled")
	ErrTenantSuspended = errors.New("route: tenant suspended or disabled")
	ErrNoHealthy       = errors.New("route: no healthy instance")
	ErrInvalidHost     = errors.New("route: invalid host")
	ErrRateLimited     = errors.New("route: rate limited")
)

// Target 是一次请求解析出的转发目标（单个 upstream）。
type Target struct {
	Scheme string // http / https
	Host   string // host:port（内部实例地址）
}

func (t *Target) URL() string {
	return fmt.Sprintf("%s://%s", t.Scheme, t.Host)
}

// PoolMember 是负载均衡池中的一个实例（跨 cache/loadbalancer 共享的公共结构）。
type PoolMember struct {
	ID       string // 实例 UUID 字符串
	Endpoint string // host:port
	Protocol string
	Weight   int
}

// MatchView 是策略匹配所需的请求上下文（Header / Path / ClientIP 子集）。
// 由数据平面从 *http.Request 提取，供支持策略分流的 Resolver 消费。
type MatchView struct {
	Header   map[string]string // 原始 header（key 小写）
	Path     string
	ClientIP string // 客户端 IP（ip scope 限流用）
}

// ContextResolver 是可选接口：支持基于请求上下文（Header/Path）分流的 Resolver。
// Resolver 若实现它，数据平面会传入真实请求信息；否则退化为无上下文 Resolve。
type ContextResolver interface {
	ResolveWith(ctx context.Context, host string, mv MatchView) (*Target, error)
}

// Resolver 把请求的 hostname 解析为转发目标。
type Resolver interface {
	// Resolve 返回 host 对应的 Target。
	// 返回 ErrNotFound 表示未绑定域名；其他业务错误（如租户停用）
	// 由实现定义，S6 起通过 *pkg.AppError 表达。
	Resolve(ctx context.Context, host string) (*Target, error)
}
