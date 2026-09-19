// Package netpools 是项目级 IP 池（IPAM）的存储与分配层。
//
// 权威在 CM 数据库（cm_node_network_pools / cm_project_networks），Docker 只做执行。
// 分配以 PostgreSQL 行锁预占、唯一约束兜底，支持多 CM 实例并存。
package netpools

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/NeoPlayful/maple-gateway/server/internal/containermanager/ipam"
	"github.com/google/uuid"
)

// 池状态。
const (
	StatusActive    = "active"    // 允许新项目分配
	StatusDraining  = "draining"  // 已有项目继续用，新项目禁止
	StatusDisabled  = "disabled"  // 完全停用
	StatusExhausted = "exhausted" // 无可用子网
	StatusConflict  = "conflict"  // 与宿主网络/Docker 网络重叠
)

// Pool 是一个节点的网络池。
type Pool struct {
	ID                 string    `json:"id"`
	NodeID             string    `json:"node_id"`
	Name               string    `json:"name"`
	Description        string    `json:"description,omitempty"`
	AddressPool        string    `json:"address_pool"`
	ProjectPrefix      int       `json:"project_prefix"`
	Priority           int       `json:"priority"`
	Status             string    `json:"status"`
	NextIndex          int64     `json:"next_index"`
	ReuseEnabled       bool      `json:"reuse_enabled"`
	ReuseDelaySeconds  int       `json:"reuse_delay_seconds"`
	IsSystemDefault    bool      `json:"is_system_default"`
	LastCheckedAt      time.Time `json:"last_checked_at,omitempty"`
	LastConflictReason string    `json:"last_conflict_reason,omitempty"`
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"`
}

// nullTime 把零值 time.Time 映射为 SQL NULL，其余原样。
func nullTime(t time.Time) sql.NullTime {
	if t.IsZero() {
		return sql.NullTime{}
	}
	return sql.NullTime{Time: t, Valid: true}
}

// CIDR 返回解析后的池网段。
func (p Pool) CIDR() (ipam.CIDR, error) { return ipam.Parse(p.AddressPool) }

// CapacityInt64 返回池在本项目前缀下可切分的子网数。
func (p Pool) CapacityInt64() (int64, error) {
	c, err := p.CIDR()
	if err != nil {
		return 0, err
	}
	return c.Capacity(p.ProjectPrefix)
}

// PoolStore 是网络池存储。
type PoolStore struct {
	db *sql.DB

	mu    sync.RWMutex
	pools map[string]Pool
}

// NewPoolStore 构造。db 为空则退化为进程内存储。
func NewPoolStore(db *sql.DB) *PoolStore {
	return &PoolStore{db: db, pools: map[string]Pool{}}
}

// Load 从数据库装载全部池（无数据库时为无操作）。
func (s *PoolStore) Load(ctx context.Context) error {
	if s.db == nil {
		return nil
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id::text, node_id, name, COALESCE(description,''), address_pool, project_prefix,
		       priority, status, next_index, reuse_enabled, reuse_delay_seconds, is_system_default,
		       last_checked_at, COALESCE(last_conflict_reason,''), created_at, updated_at
		FROM cm_node_network_pools`)
	if err != nil {
		return fmt.Errorf("load cm_node_network_pools: %w", err)
	}
	defer rows.Close()
	s.mu.Lock()
	defer s.mu.Unlock()
	for rows.Next() {
		var p Pool
		var lastChecked sql.NullTime
		if err := rows.Scan(&p.ID, &p.NodeID, &p.Name, &p.Description, &p.AddressPool, &p.ProjectPrefix,
			&p.Priority, &p.Status, &p.NextIndex, &p.ReuseEnabled, &p.ReuseDelaySeconds, &p.IsSystemDefault,
			&lastChecked, &p.LastConflictReason, &p.CreatedAt, &p.UpdatedAt); err != nil {
			return fmt.Errorf("scan cm_node_network_pool: %w", err)
		}
		p.LastCheckedAt = lastChecked.Time
		s.pools[p.ID] = p
	}
	return rows.Err()
}

// Create 新增一个池并落库。校验 CIDR 合法、前缀合法、与保留段不冲突。
func (s *PoolStore) Create(ctx context.Context, p Pool) (Pool, error) {
	if p.ID == "" {
		p.ID = uuid.NewString()
	}
	if p.Status == "" {
		p.Status = StatusActive
	}
	if p.Priority == 0 {
		p.Priority = 100
	}
	// 复用与冷却期沿用文档默认（启用 / 600s）：新建池一律启用复用，停用改由 Update 显式设置。
	// bool 零值无法区分「未指定」与「显式停用」，故在创建这一唯一入口统一置为启用。
	p.ReuseEnabled = true
	if p.ReuseDelaySeconds == 0 {
		p.ReuseDelaySeconds = 600
	}
	poolCIDR, err := ipam.Parse(p.AddressPool)
	if err != nil {
		return Pool{}, fmt.Errorf("地址池非法: %w", err)
	}
	if _, err := poolCIDR.Capacity(p.ProjectPrefix); err != nil {
		return Pool{}, fmt.Errorf("项目前缀非法: %w", err)
	}
	if _, err := ipam.ValidatePool(poolCIDR); err != nil {
		return Pool{}, fmt.Errorf("地址池不可用: %w", err)
	}
	// 同节点内池之间不得重叠（不同节点允许相同 CIDR）。
	s.mu.RLock()
	for _, other := range s.pools {
		if other.NodeID != p.NodeID {
			continue
		}
		oc, err := other.CIDR()
		if err != nil {
			continue
		}
		if oc.Overlaps(poolCIDR) {
			s.mu.RUnlock()
			return Pool{}, fmt.Errorf("与同节点池 %s(%s) 重叠", other.Name, other.AddressPool)
		}
	}
	s.mu.RUnlock()

	now := time.Now()
	p.CreatedAt, p.UpdatedAt = now, now
	if err := s.persist(ctx, p); err != nil {
		return Pool{}, err
	}
	s.mu.Lock()
	s.pools[p.ID] = p
	s.mu.Unlock()
	return p, nil
}

// Adopt 把事务内已落库的池状态写入内存视图（不再落库）。
func (s *PoolStore) Adopt(p Pool) {
	s.mu.Lock()
	s.pools[p.ID] = p
	s.mu.Unlock()
}

// Get 取一个池。
func (s *PoolStore) Get(id string) (Pool, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	p, ok := s.pools[id]
	return p, ok
}

// ListByNode 返回某节点的全部池，按 priority ASC（同级按创建时间）。
func (s *PoolStore) ListByNode(nodeID string) []Pool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Pool, 0, len(s.pools))
	for _, p := range s.pools {
		if p.NodeID == nodeID {
			out = append(out, p)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Priority != out[j].Priority {
			return out[i].Priority < out[j].Priority
		}
		return out[i].CreatedAt.Before(out[j].CreatedAt)
	})
	return out
}

// List 返回全部池。
func (s *PoolStore) List() []Pool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Pool, 0, len(s.pools))
	for _, p := range s.pools {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out
}

// Update 更新池的可变字段并落库。含在用池 CIDR 不变更的护栏由调用方保证（见 API 层）。
func (s *PoolStore) Update(ctx context.Context, p Pool) (Pool, error) {
	s.mu.Lock()
	old, ok := s.pools[p.ID]
	if !ok {
		s.mu.Unlock()
		return Pool{}, fmt.Errorf("池不存在")
	}
	old.Name = p.Name
	old.Description = p.Description
	old.Priority = p.Priority
	old.Status = p.Status
	old.ReuseEnabled = p.ReuseEnabled
	old.ReuseDelaySeconds = p.ReuseDelaySeconds
	old.UpdatedAt = time.Now()
	s.pools[p.ID] = old
	s.mu.Unlock()
	if err := s.persist(ctx, old); err != nil {
		return Pool{}, err
	}
	return old, nil
}

// SetStatus 更新池状态（供对账与人工操作）。
func (s *PoolStore) SetStatus(ctx context.Context, id, status, conflictReason string) error {
	s.mu.Lock()
	p, ok := s.pools[id]
	if !ok {
		s.mu.Unlock()
		return fmt.Errorf("池不存在")
	}
	p.Status = status
	p.LastConflictReason = conflictReason
	p.UpdatedAt = time.Now()
	s.pools[id] = p
	s.mu.Unlock()
	return s.persist(ctx, p)
}

// Delete 删除一个池（在用判定由调用方完成）。
func (s *PoolStore) Delete(ctx context.Context, id string) error {
	if s.db != nil {
		if _, err := s.db.ExecContext(ctx, `DELETE FROM cm_node_network_pools WHERE id=$1::uuid`, id); err != nil {
			return fmt.Errorf("删除网络池: %w", err)
		}
	}
	s.mu.Lock()
	delete(s.pools, id)
	s.mu.Unlock()
	return nil
}

func (s *PoolStore) persist(ctx context.Context, p Pool) error {
	if s.db == nil {
		return nil
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO cm_node_network_pools (id, node_id, name, description, address_pool, project_prefix,
			priority, status, next_index, reuse_enabled, reuse_delay_seconds, is_system_default,
			last_checked_at, last_conflict_reason, created_at, updated_at)
		VALUES ($1::uuid, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12,
			$13, $14, $15, $16)
		ON CONFLICT (id) DO UPDATE SET
			name=EXCLUDED.name, description=EXCLUDED.description, priority=EXCLUDED.priority,
			status=EXCLUDED.status, next_index=EXCLUDED.next_index,
			reuse_enabled=EXCLUDED.reuse_enabled, reuse_delay_seconds=EXCLUDED.reuse_delay_seconds,
			last_checked_at=EXCLUDED.last_checked_at, last_conflict_reason=EXCLUDED.last_conflict_reason,
			updated_at=EXCLUDED.updated_at`,
		p.ID, p.NodeID, p.Name, p.Description, p.AddressPool, p.ProjectPrefix,
		p.Priority, p.Status, p.NextIndex, p.ReuseEnabled, p.ReuseDelaySeconds, p.IsSystemDefault,
		nullTime(p.LastCheckedAt), p.LastConflictReason, p.CreatedAt, p.UpdatedAt)
	if err != nil {
		return fmt.Errorf("保存网络池: %w", err)
	}
	return nil
}
