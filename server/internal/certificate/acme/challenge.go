// Package acme 封装 ACME（Let's Encrypt / 兼容 CA）自动签发与续期客户端。
//
// 设计约束：
//   - 账户密钥与证书私钥均加密落库（由注入的 AccountStore 实现负责），明细格式绝不出现在日志/API。
//   - http-01 challenge 由数据平面代答：本包提供内存 ChallengeStore，网关在路由前短路读取。
//   - 本包不做落库/缓存/审计（证书落库由 certificate.Service 编排）。
package acme

import (
	"strings"
	"sync"
	"time"
)

// http01Prefix 是 ACME http-01 挑战的 URL 前缀（RFC 8555 §8.3）。
const http01Prefix = "/.well-known/acme-challenge/"

// ChallengeStore 是 http-01 挑战的内存存储：token → keyAuthorization。
// 数据平面每一请求按 path 查询；纯内存、带 TTL，严禁访问 DB（与握手热路径同原则）。
type ChallengeStore struct {
	mu    sync.RWMutex
	byTok map[string]entry
}

type entry struct {
	keyAuth string
	expires time.Time
}

// NewChallengeStore 构造空 store。
func NewChallengeStore() *ChallengeStore {
	return &ChallengeStore{byTok: make(map[string]entry)}
}

// Present 登记一个 challenge 应答（keyAuthorization），ttl 后自动过期。
func (s *ChallengeStore) Present(token, keyAuth string, ttl time.Duration) {
	if token == "" || keyAuth == "" {
		return
	}
	if ttl <= 0 {
		ttl = 10 * time.Minute
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.byTok[token] = entry{keyAuth: keyAuth, expires: time.Now().Add(ttl)}
}

// Clear 移除某 token 的挑战（验证完成或失败后）。
func (s *ChallengeStore) Clear(token string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.byTok, token)
}

// Lookup 按 token 取 keyAuthorization；未命中或已过期返回 ("", false)。
func (s *ChallengeStore) Lookup(token string) (string, bool) {
	s.mu.RLock()
	e, ok := s.byTok[token]
	s.mu.RUnlock()
	if !ok || time.Now().After(e.expires) {
		return "", false
	}
	return e.keyAuth, true
}

// RespondPath 按请求 path 应答 http-01 挑战：命中返回 (keyAuth, true)；
// 非挑战前缀 / 未命中 / 已过期返回 ("", false)，调用方应回落常规路由。
// 实现 gateway.ChallengeResponder 语义。
func (s *ChallengeStore) RespondPath(path string) (string, bool) {
	if !strings.HasPrefix(path, http01Prefix) {
		return "", false
	}
	token := strings.TrimPrefix(path, http01Prefix)
	if token == "" || strings.Contains(token, "/") {
		return "", false
	}
	return s.Lookup(token)
}

// Len 当前登记的挑战数（含未清理的过期项，运维/测试用）。
func (s *ChallengeStore) Len() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.byTok)
}

// Reap 清理过期挑战，返回清理数量。
func (s *ChallengeStore) Reap() int {
	now := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for k, v := range s.byTok {
		if now.After(v.expires) {
			delete(s.byTok, k)
			n++
		}
	}
	return n
}
