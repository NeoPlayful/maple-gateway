// Package enrollment 承载节点注册准入：一次性 Enrollment Token 的签发/消费，
// 以及注册成功后为节点发放独立的 Node Credential（后续重连凭此鉴权）。
package enrollment

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"
)

// sha256Hex 返回明文的 SHA-256 十六进制摘要（用于已消费 Token 的占位值）。
func sha256Hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// TokenStatus 是 Token 的生命周期状态。
type TokenStatus string

const (
	TokenActive  TokenStatus = "active"  // 未使用且未过期
	TokenUsed    TokenStatus = "used"    // 已被消费
	TokenExpired TokenStatus = "expired" // 已过期（展示态，按 expires_at 派生）
	TokenRevoked TokenStatus = "revoked" // 被管理端撤销
)

// ErrTokenInvalid 表示 Token 不存在或不可用。
var ErrTokenInvalid = errors.New("enrollment token invalid")

// Token 是一次性注册令牌。
type Token struct {
	ID        string      `json:"id"`
	Value     string      `json:"value"`
	Note      string      `json:"note,omitempty"`
	Status    TokenStatus `json:"status"`
	CreatedBy string      `json:"created_by,omitempty"`
	CreatedMs int64       `json:"created_at_ms"`
	ExpiresMs int64       `json:"expires_at_ms"`
	UsedMs    int64       `json:"used_at_ms,omitempty"`
	UsedBy    string      `json:"used_by_node_id,omitempty"`
}

// Expired 报告在给定时刻 Token 是否已过期。
func (t *Token) Expired(now time.Time) bool {
	return t.ExpiresMs > 0 && now.UnixMilli() >= t.ExpiresMs
}

// DisplayStatus 返回展示态：used/revoked 优先，其次按过期时间派生。
func (t *Token) DisplayStatus(now time.Time) TokenStatus {
	switch t.Status {
	case TokenUsed, TokenRevoked:
		return t.Status
	}
	if t.Expired(now) {
		return TokenExpired
	}
	return TokenActive
}

// TokenStore 是一次性 Token 的存储。配置了数据库则写穿 cm_enrollment_tokens
// （签发/撤销/消费即时落库，重启不丢）；否则退化为进程内存储（开发用）。
//
// value 以明文存储：便于管理端在 Token 被消费前回显并复制给运维装机。消费时即把
// 明文置空（写入哈希占位），避免已用 Token 残留可用凭证。
type TokenStore struct {
	db *sql.DB

	mu     sync.RWMutex
	tokens map[string]*Token // value → Token
}

// NewTokenStore 构造空 Token 存储（无 DB，进程内）。
func NewTokenStore() *TokenStore {
	return &TokenStore{tokens: make(map[string]*Token)}
}

// NewTokenStoreDB 构造带持久化的 Token 存储；db 为空则退化为进程内。
func NewTokenStoreDB(db *sql.DB) *TokenStore {
	return &TokenStore{db: db, tokens: make(map[string]*Token)}
}

// Load 从数据库装载 Token（无 DB 时为无操作）。
func (s *TokenStore) Load(ctx context.Context) error {
	if s.db == nil {
		return nil
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, value, note, status, COALESCE(used_by::text,''), created_at, expires_at,
		       COALESCE(used_at, to_timestamp(0))
		FROM cm_enrollment_tokens`)
	if err != nil {
		return fmt.Errorf("load cm_enrollment_tokens: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var (
			t              Token
			status         string
			created, expire time.Time
			used           time.Time
		)
		if err := rows.Scan(&t.ID, &t.Value, &t.Note, &status, &t.UsedBy, &created, &expire, &used); err != nil {
			return fmt.Errorf("scan cm_enrollment_token: %w", err)
		}
		t.Status = TokenStatus(status)
		t.CreatedMs = created.UnixMilli()
		t.ExpiresMs = expire.UnixMilli()
		if !used.IsZero() {
			t.UsedMs = used.UnixMilli()
		}
		// value 消费后为空：以 ID 作为占位键，保证已用 Token 仍可被列出。
		key := t.Value
		if key == "" {
			key = t.ID
		}
		s.tokens[key] = &t
	}
	return rows.Err()
}

// Issue 签发一个限时一次性 Token。ttl <= 0 时默认 24 小时。
func (s *TokenStore) Issue(note string, ttl time.Duration) (*Token, error) {
	return s.IssueBy(note, ttl, "")
}

// IssueBy 签发 Token 并记录签发人（createdBy 可为空）。
func (s *TokenStore) IssueBy(note string, ttl time.Duration, createdBy string) (*Token, error) {
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
		CreatedBy: createdBy,
		CreatedMs: now.UnixMilli(),
		ExpiresMs: now.Add(ttl).UnixMilli(),
	}
	s.mu.Lock()
	s.tokens[val] = t
	s.mu.Unlock()
	if err := s.persist(context.Background(), t); err != nil {
		return nil, err
	}
	return t, nil
}

// Get 按 Token 值返回（含已用/过期，供展示）。
func (s *TokenStore) Get(value string) (*Token, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	t, ok := s.tokens[value]
	return t, ok
}

// List 返回全部 Token，按签发时间倒序（最新在前）。
func (s *TokenStore) List() []*Token {
	s.mu.RLock()
	out := make([]*Token, 0, len(s.tokens))
	for _, t := range s.tokens {
		out = append(out, t)
	}
	s.mu.RUnlock()
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedMs > out[j].CreatedMs })
	return out
}

// Peek 无副作用地校验 Token 可用性：存在、未过期、未使用、未撤销。
// 用于准入前的判断，不改变状态。
func (s *TokenStore) Peek(value string) error {
	s.mu.RLock()
	t, ok := s.tokens[value]
	s.mu.RUnlock()
	if !ok || t.Status == TokenRevoked {
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
// value 改为写哈希占位（明文不下库），重复调用返回 ErrTokenInvalid。
// 有 DB 时以 UPDATE … WHERE status='active' 的影响行数作为一次性判据，
// 保证并发/多实例下不会被双消费。
func (s *TokenStore) Consume(value, nodeID string) error {
	if s.db != nil {
		return s.consumeDB(value, nodeID)
	}
	if err := s.Peek(value); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	t := s.tokens[value]
	if t.Status != TokenActive || t.Expired(time.Now()) {
		return ErrTokenInvalid
	}
	t.Status = TokenUsed
	t.UsedMs = time.Now().UnixMilli()
	t.UsedBy = nodeID
	return nil
}

// consumeDB 走数据库做原子消费：只有 status='active' 且未过期的一行会被更新。
func (s *TokenStore) consumeDB(value, nodeID string) error {
	now := time.Now()
	res, err := s.db.ExecContext(context.Background(), `
		UPDATE cm_enrollment_tokens
		SET status='used', used_by=$2::uuid, used_at=now(), value=$3
		WHERE value=$1 AND status='active' AND (expires_at IS NULL OR expires_at > now())`,
		value, nodeID, hashPlaceholder(value))
	if err != nil {
		return fmt.Errorf("consume enrollment token: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("consume enrollment token: %w", err)
	}
	if n == 0 {
		return ErrTokenInvalid
	}
	// 同步内存态：明文 value 键改为哈希占位键。
	s.mu.Lock()
	if t, ok := s.tokens[value]; ok {
		delete(s.tokens, value)
		t.Status = TokenUsed
		t.UsedMs = now.UnixMilli()
		t.UsedBy = nodeID
		t.Value = hashPlaceholder(value)
		s.tokens[t.ID] = t
	}
	s.mu.Unlock()
	return nil
}

// Revoke 撤销未使用的签发：状态置 revoked（保留记录供审计），不物理删除。
func (s *TokenStore) Revoke(id string) bool {
	s.mu.Lock()
	var target *Token
	for _, t := range s.tokens {
		if t.ID == id {
			target = t
			break
		}
	}
	if target == nil || target.Status != TokenActive {
		s.mu.Unlock()
		return false
	}
	target.Status = TokenRevoked
	s.mu.Unlock()
	if s.db != nil {
		_, _ = s.db.ExecContext(context.Background(),
			`UPDATE cm_enrollment_tokens SET status='revoked' WHERE id=$1 AND status='active'`, id)
	}
	return true
}

// persist 插入一条 Token 记录（无 DB 时为无操作）。
func (s *TokenStore) persist(ctx context.Context, t *Token) error {
	if s.db == nil {
		return nil
	}
	expire := time.UnixMilli(t.ExpiresMs)
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO cm_enrollment_tokens (id, value, note, status, created_at, expires_at)
		VALUES ($1,$2,$3,$4,to_timestamp($5/1000.0), $6)`,
		t.ID, t.Value, t.Note, string(TokenActive), t.CreatedMs, expire)
	if err != nil {
		return fmt.Errorf("persist enrollment token: %w", err)
	}
	return nil
}

// hashPlaceholder 为已消费 Token 生成不可复用的占位值（保留唯一性、去明文）。
func hashPlaceholder(value string) string {
	sum := sha256Hex(value)
	return "used:" + sum[:32]
}

// randomToken 生成带前缀的随机令牌（16 字节熵）。
func randomToken(prefix string) (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return prefix + hex.EncodeToString(b), nil
}
