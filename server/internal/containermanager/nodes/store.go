// Package nodes 是 Container Manager 侧的节点权威模型。
//
// 节点身份不在本包重复：Gateway 的 nodes 表是唯一节点权威（name/host/region/
// labels/weight/路由状态），本包以 **Gateway 节点 UUID（字符串）作为 node_id**，
// 只持有 CM 侧运行期事实：凭证哈希、OS/arch/Agent 版本、在线状态机。
//
// 合并了原先分散的两处：凭证校验（原 enrollment.NodeStore）与在线状态机
// （原 nodestate.Tracker）。配置了数据库则写穿 cm_node_runtime；否则退化为
// 进程内存储（开发用）。
package nodes

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
	"time"
)

// State 是节点在线状态（由 WS 会话 + 心跳驱动，区别于 Gateway 侧的路由状态）。
type State string

const (
	Online   State = "online"
	Unstable State = "unstable"
	Offline  State = "offline"
)

// 在线状态阈值。
const (
	DefaultUnstableAfter = 30 * time.Second
	DefaultOfflineAfter  = 90 * time.Second
)

// lastSeenWriteInterval 是 last_seen_at 落库的最小间隔，避免每次心跳都写库。
const lastSeenWriteInterval = 15 * time.Second

// ErrUnknown 表示节点不存在或凭证不匹配。
var ErrUnknown = errors.New("node unknown or credential mismatch")

// Runtime 是某节点在 CM 侧的运行期记录。ID 为 Gateway 节点 UUID 字符串。
type Runtime struct {
	ID             string
	Name           string
	CredentialHash string
	OS             string
	Arch           string
	AgentVersion   string
	Revoked        bool
	RegisteredAt   time.Time
	LastSeenAt     time.Time

	state         State
	connected     bool
	lastSeenWrite time.Time
}

// State 返回节点当前在线状态。
func (r *Runtime) State() State { return r.state }

// Connected 报告节点当前是否有活跃会话。
func (r *Runtime) Connected() bool { return r.connected }

// Store 是节点运行期记录表。
type Store struct {
	db            *sql.DB
	unstableAfter time.Duration
	offlineAfter  time.Duration

	mu    sync.RWMutex
	items map[string]*Runtime
}

// New 构造节点存储。db 为空则退化为进程内存储。
func New(db *sql.DB, unstableAfter, offlineAfter time.Duration) *Store {
	if unstableAfter <= 0 {
		unstableAfter = DefaultUnstableAfter
	}
	if offlineAfter <= 0 {
		offlineAfter = DefaultOfflineAfter
	}
	return &Store{
		db:            db,
		unstableAfter: unstableAfter,
		offlineAfter:  offlineAfter,
		items:         make(map[string]*Runtime),
	}
}

// Load 从数据库装载节点运行期记录（无数据库时为无操作）。
func (s *Store) Load(ctx context.Context) error {
	if s.db == nil {
		return nil
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id::text, name, credential_hash, os, arch, agent_version, revoked, registered_at,
		       COALESCE(last_seen_at, to_timestamp(0))
		FROM cm_node_runtime`)
	if err != nil {
		return fmt.Errorf("load cm_node_runtime: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		r := &Runtime{state: Offline}
		if err := rows.Scan(&r.ID, &r.Name, &r.CredentialHash, &r.OS, &r.Arch, &r.AgentVersion,
			&r.Revoked, &r.RegisteredAt, &r.LastSeenAt); err != nil {
			return fmt.Errorf("scan cm_node_runtime: %w", err)
		}
		s.items[r.ID] = r
	}
	return rows.Err()
}

// Ensure 创建或刷新节点运行期记录并发放新凭证。
// id 为 Gateway 节点 UUID 字符串，name 为节点登记名（= Agent hostname）。
// 已存在则更新静态字段、换发凭证、清除吊销。返回明文凭证。
func (s *Store) Ensure(ctx context.Context, id, name, osName, arch, agentVersion string) (string, error) {
	cred, err := randomToken("mg_node_")
	if err != nil {
		return "", err
	}
	h := hashCredential(cred)
	now := time.Now()

	s.mu.Lock()
	r, ok := s.items[id]
	if !ok {
		r = &Runtime{ID: id, RegisteredAt: now}
		s.items[id] = r
	}
	if name != "" {
		r.Name = name
	}
	r.CredentialHash = h
	r.OS = osName
	r.Arch = arch
	r.AgentVersion = agentVersion
	r.Revoked = false
	r.LastSeenAt = now
	r.state = Online
	r.connected = true
	r.lastSeenWrite = now
	s.mu.Unlock()

	if err := s.persist(ctx, r); err != nil {
		return "", err
	}
	return cred, nil
}

// Get 按 ID 取节点运行期记录。
func (s *Store) Get(id string) (*Runtime, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	r, ok := s.items[id]
	return r, ok
}

// List 返回全部节点运行期记录（顺序不定）。
func (s *Store) List() []*Runtime {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*Runtime, 0, len(s.items))
	for _, r := range s.items {
		out = append(out, r)
	}
	return out
}

// VerifyCredential 校验节点凭证（哈希后等长比较）。
func (s *Store) VerifyCredential(id, cred string) error {
	s.mu.RLock()
	r, ok := s.items[id]
	s.mu.RUnlock()
	if !ok || r.Revoked {
		return ErrUnknown
	}
	if r.CredentialHash == "" || r.CredentialHash != hashCredential(cred) {
		return ErrUnknown
	}
	return nil
}

// Revoke 吊销节点凭证（记录保留，标记吊销）。
func (s *Store) Revoke(ctx context.Context, id string) bool {
	s.mu.Lock()
	r, ok := s.items[id]
	if ok {
		r.Revoked = true
		r.CredentialHash = ""
	}
	s.mu.Unlock()
	if ok && s.db != nil {
		_, _ = s.db.ExecContext(ctx,
			`UPDATE cm_node_runtime SET revoked=true, credential_hash='', updated_at=now() WHERE id=$1::uuid`, id)
	}
	return ok
}

// UpdateHello 用最近一次握手刷新节点运行期字段。
func (s *Store) UpdateHello(id, osName, arch, agentVersion string) {
	s.mu.Lock()
	r, ok := s.items[id]
	if ok {
		if osName != "" {
			r.OS = osName
		}
		if arch != "" {
			r.Arch = arch
		}
		if agentVersion != "" {
			r.AgentVersion = agentVersion
		}
	}
	s.mu.Unlock()
	if ok && s.db != nil {
		_, _ = s.db.ExecContext(context.Background(),
			`UPDATE cm_node_runtime SET os=$2, arch=$3, agent_version=$4, updated_at=now() WHERE id=$1::uuid`,
			id, osName, arch, agentVersion)
	}
}

// Touch 记录一次活跃（会话建立或心跳到达），状态置 Online。
func (s *Store) Touch(id string) {
	now := time.Now()
	var write bool
	s.mu.Lock()
	if r, ok := s.items[id]; ok {
		r.LastSeenAt = now
		r.state = Online
		r.connected = true
		if now.Sub(r.lastSeenWrite) >= lastSeenWriteInterval {
			r.lastSeenWrite = now
			write = true
		}
	}
	s.mu.Unlock()
	if write && s.db != nil {
		_, _ = s.db.ExecContext(context.Background(),
			`UPDATE cm_node_runtime SET last_seen_at=now() WHERE id=$1::uuid`, id)
	}
}

// Disconnect 标记会话断开：保留 lastSeen，由 Evaluate 推进到 Unstable/Offline。
func (s *Store) Disconnect(id string) {
	s.mu.Lock()
	if r, ok := s.items[id]; ok {
		r.connected = false
	}
	s.mu.Unlock()
}

// State 返回节点当前状态；未知节点视为 Offline。
func (s *Store) State(id string) State {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if r, ok := s.items[id]; ok {
		return r.state
	}
	return Offline
}

// Online 报告节点当前是否处于在线态。
func (s *Store) Online(id string) bool {
	return s.State(id) == Online
}

// Evaluate 依据当前时间推进所有节点状态，返回状态发生变化的节点。
func (s *Store) Evaluate() map[string]State {
	now := time.Now()
	changed := make(map[string]State)
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, r := range s.items {
		var want State
		elapsed := now.Sub(r.LastSeenAt)
		switch {
		case elapsed >= s.offlineAfter:
			want = Offline
		case elapsed >= s.unstableAfter:
			want = Unstable
		default:
			want = Online
		}
		if want != r.state {
			r.state = want
			changed[id] = want
		}
	}
	return changed
}

// Runner 周期推进状态机，直到 ctx 取消。
func (s *Store) Runner(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = 15 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.Evaluate()
		}
	}
}

func (s *Store) persist(ctx context.Context, r *Runtime) error {
	if s.db == nil {
		return nil
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO cm_node_runtime (id, name, credential_hash, os, arch, agent_version, revoked,
			registered_at, last_seen_at, updated_at)
		VALUES ($1::uuid,$2,$3,$4,$5,$6,false,now(),now(),now())
		ON CONFLICT (id) DO UPDATE SET
			name=EXCLUDED.name,
			credential_hash=EXCLUDED.credential_hash, os=EXCLUDED.os, arch=EXCLUDED.arch,
			agent_version=EXCLUDED.agent_version, revoked=false,
			last_seen_at=now(), updated_at=now()`,
		r.ID, r.Name, r.CredentialHash, r.OS, r.Arch, r.AgentVersion)
	if err != nil {
		return fmt.Errorf("persist cm_node_runtime: %w", err)
	}
	return nil
}

func hashCredential(cred string) string {
	sum := sha256.Sum256([]byte(cred))
	return hex.EncodeToString(sum[:])
}

func randomToken(prefix string) (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return prefix + hex.EncodeToString(b), nil
}
