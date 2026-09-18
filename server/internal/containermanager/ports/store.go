// Package ports 集中分配本机端口，杜绝多项目、多版本在同一节点上抢占同一宿主端口。
//
// 分配方是控制面（CM），索取与归还经内部接口（Gateway 代理）：占位落库 cm_port_allocations，
// 进程内另存一份已占端口集合与「资源 → 端口」反查表，分配时查内存避开实时占用，
// 由库的唯一约束兜底跨进程冲突。port 取全局唯一（而非按节点唯一）：单节点部署下即为零冲突，
// 多节点下偏保守，宁少用不撞端口。
package ports

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"sync"

	"github.com/google/uuid"
)

// Store 是端口占用的存储与分配器。
type Store struct {
	db  *sql.DB
	low int
	// high 为区间闭区间上界。
	high int

	mu   sync.Mutex
	used map[int]struct{}
	// byResource 是「占用方 → 已占端口」反查表，供按资源整批释放。
	byResource map[string][]int
}

// NewStore 构造。db 为空则退化为进程内存储（重启即失忆，仅开发用）。
// low/high 为可用端口区间（闭区间）；非法（low<=0 或 high<low）时回退到默认区间。
func NewStore(db *sql.DB, low, high int) *Store {
	if low <= 0 || high < low {
		low, high = DefaultPortRange()
	}
	return &Store{
		db: db, low: low, high: high,
		used:       map[int]struct{}{},
		byResource: map[string][]int{},
	}
}

// DefaultPortRange 返回默认可用端口区间。
func DefaultPortRange() (int, int) { return 20000, 30000 }

// Load 从数据库装载既有占用（无数据库时为无操作）。
func (s *Store) Load(ctx context.Context) error {
	if s.db == nil {
		return nil
	}
	rows, err := s.db.QueryContext(ctx, `SELECT port, resource_id FROM cm_port_allocations`)
	if err != nil {
		return fmt.Errorf("load port allocations: %w", err)
	}
	defer rows.Close()
	s.mu.Lock()
	defer s.mu.Unlock()
	for rows.Next() {
		var (
			p  int
			ra string
		)
		if err := rows.Scan(&p, &ra); err != nil {
			return fmt.Errorf("scan port allocation: %w", err)
		}
		s.used[p] = struct{}{}
		s.byResource[ra] = append(s.byResource[ra], p)
	}
	return rows.Err()
}

// Allocate 为某资源分配一个空闲端口。resourceID 是占用方标识（如项目 ID），
// 释放时按它整批归还；kind 记录占用类型（如 project），nodeID 为落点（可空）。
// 幂等：同资源已有占用时直接返回既有端口，重复实例化不会泄漏端口。区间耗尽返回错误。
func (s *Store) Allocate(ctx context.Context, resourceID, kind, nodeID string) (int, error) {
	if resourceID == "" {
		return 0, fmt.Errorf("缺少占用方标识")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if ps := s.byResource[resourceID]; len(ps) > 0 {
		return ps[0], nil
	}
	for p := s.low; p <= s.high; p++ {
		if _, taken := s.used[p]; taken {
			continue
		}
		if err := s.reserve(ctx, p, resourceID, kind, nodeID); err != nil {
			return 0, err
		}
		s.used[p] = struct{}{}
		s.byResource[resourceID] = append(s.byResource[resourceID], p)
		return p, nil
	}
	return 0, fmt.Errorf("端口区间 %d-%d 已耗尽", s.low, s.high)
}

// reserve 把一个端口写入库；无数据库时跳过（纯内存模式）。
func (s *Store) reserve(ctx context.Context, port int, resourceID, kind, nodeID string) error {
	if s.db == nil {
		return nil
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO cm_port_allocations (id, port, node_id, resource_id, kind, created_at)
		VALUES ($1::uuid, $2, NULLIF($3,'')::uuid, $4, $5, now())`,
		uuid.NewString(), port, nodeID, resourceID, kind)
	if err != nil {
		return fmt.Errorf("记录端口占用: %w", err)
	}
	return nil
}

// Release 归还某资源的全部端口占用；无占用时为无操作。
func (s *Store) Release(ctx context.Context, resourceID string) error {
	if resourceID == "" {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db != nil {
		if _, err := s.db.ExecContext(ctx,
			`DELETE FROM cm_port_allocations WHERE resource_id=$1`, resourceID); err != nil {
			return fmt.Errorf("释放端口占用: %w", err)
		}
	}
	for _, p := range s.byResource[resourceID] {
		delete(s.used, p)
	}
	delete(s.byResource, resourceID)
	return nil
}

// Allocated 返回当前已占端口（升序），供诊断。
func (s *Store) Allocated() []int {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]int, 0, len(s.used))
	for p := range s.used {
		out = append(out, p)
	}
	sort.Ints(out)
	return out
}
