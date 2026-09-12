// Package enrollment 承载节点注册准入：一次性 Enrollment Token 的签发/消费，
// 以及注册成功后为节点发放独立的 Node Credential（后续重连凭此鉴权）。
package enrollment

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"time"
)

// TokenStatus 是 Token 的生命周期状态。
type TokenStatus string

const (
	TokenActive  TokenStatus = "active"  // 未使用且未过期
	TokenUsed    TokenStatus = "used"    // 已被消费
	TokenExpired TokenStatus = "expired" // 已过期
)

// ErrTokenInvalid 表示 Token 不存在或不可用。
var ErrTokenInvalid = errors.New("enrollment token invalid")

// Token 是一次性注册令牌。
type Token struct {
	ID        string      `json:"id"`
	Value     string      `json:"value"`
	Note      string      `json:"note,omitempty"`
	Status    TokenStatus `json:"status"`
	CreatedMs int64       `json:"created_at_ms"`
	ExpiresMs int64       `json:"expires_at_ms"`
	UsedMs    int64       `json:"used_at_ms,omitempty"`
	UsedBy    string      `json:"used_by_node_id,omitempty"`
}

// Expired 报告在给定时刻 Token 是否已过期。
func (t *Token) Expired(now time.Time) bool {
	return t.ExpiresMs > 0 && now.UnixMilli() >= t.ExpiresMs
}

// TokenStore 是 Token 的内存存储。
type TokenStore struct {
	tokens map[string]*Token
}

// NewTokenStore 构造空 Token 存储。
func NewTokenStore() *TokenStore {
	return &TokenStore{tokens: make(map[string]*Token)}
}

// Issue 签发一个限时一次性 Token。ttl <= 0 时默认 24 小时。
func (s *TokenStore) Issue(note string, ttl time.Duration) (*Token, error) {
	if ttl <= 0 {
		ttl = 24 * time.Hour
	}
	now := time.Now()
	val, err := randomToken("mg_enroll_")
	if err != nil {
		return nil, err
	}
	id, err := randomToken("tok_")
	if err != nil {
		return nil, err
	}
	t := &Token{
		ID:        id,
		Value:     val,
		Note:      note,
		Status:    TokenActive,
		CreatedMs: now.UnixMilli(),
		ExpiresMs: now.Add(ttl).UnixMilli(),
	}
	s.tokens[val] = t
	return t, nil
}

// Get 按 Token 值返回（含已用/过期，供展示）。
func (s *TokenStore) Get(value string) (*Token, bool) {
	t, ok := s.tokens[value]
	return t, ok
}

// List 返回全部 Token（签发顺序不定，由调用方排序）。
func (s *TokenStore) List() []*Token {
	out := make([]*Token, 0, len(s.tokens))
	for _, t := range s.tokens {
		out = append(out, t)
	}
	return out
}

// Peek 无副作用地校验 Token 可用性：存在、未过期、未使用。
// 用于准入前的判断，不改变状态。
func (s *TokenStore) Peek(value string) error {
	t, ok := s.tokens[value]
	if !ok {
		return ErrTokenInvalid
	}
	if t.Status == TokenUsed {
		return fmt.Errorf("%w: already used", ErrTokenInvalid)
	}
	if t.Expired(time.Now()) {
		return fmt.Errorf("%w: expired", ErrTokenInvalid)
	}
	return nil
}

// Consume 原子地消费 Token：校验通过则标记为已用并绑定节点。
// 重复调用返回 ErrTokenInvalid。
func (s *TokenStore) Consume(value, nodeID string) error {
	if err := s.Peek(value); err != nil {
		return err
	}
	t := s.tokens[value]
	t.Status = TokenUsed
	t.UsedMs = time.Now().UnixMilli()
	t.UsedBy = nodeID
	return nil
}

// Revoke 删除 Token（管理端撤销未使用的签发）。
func (s *TokenStore) Revoke(id string) bool {
	for val, t := range s.tokens {
		if t.ID == id {
			delete(s.tokens, val)
			return true
		}
	}
	return false
}

// randomToken 生成带前缀的随机令牌（16 字节熵）。
func randomToken(prefix string) (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return prefix + hex.EncodeToString(b), nil
}
