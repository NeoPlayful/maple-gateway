package netpools

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
)

// 项目网络状态。
const (
	NetReserved = "reserved" // 已预占网段，Docker 网络未建
	NetCreating = "creating" // 正在创建 Docker 网络
	NetActive   = "active"   // 网络就绪，项目可用
	NetDeleting = "deleting" // 正在删除
	NetReleased = "released" // 已释放，冷却后可复用
	NetFailed   = "failed"   // 失败且保留（不释放，等重试）
	NetConflict = "conflict" // 与 Docker 实际状态冲突
	NetDrift    = "drift"    // 与 Docker 实际状态漂移
)

// ProjectNetwork 记录一个项目占用的网段及其 Docker 网络。
type ProjectNetwork struct {
	ID                string    `json:"id"`
	NodeID            string    `json:"node_id"`
	ProjectID         string    `json:"project_id"`
	PoolID            string    `json:"pool_id"`
	SubnetIndex       int64     `json:"subnet_index"`
	Subnet            string    `json:"subnet"`
	Gateway           string    `json:"gateway"`
	DockerNetworkName string    `json:"docker_network_name"`
	Status            string    `json:"status"`
	AllocatedAt       time.Time `json:"allocated_at,omitempty"`
	ReleasedAt        time.Time `json:"released_at,omitempty"`
	ReuseAfter        time.Time `json:"reuse_after,omitempty"`
	LastError         string    `json:"last_error,omitempty"`
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
}

// NetworkStore 是项目网络存储。
type NetworkStore struct {
	db *sql.DB

	mu    sync.RWMutex
	nets  map[string]ProjectNetwork // 按项目 ID 索引（MVP 一项目一网段）
	bySub map[string]ProjectNetwork // 按 "node_id|subnet" 索引，供并发占用检查
}

// NewNetworkStore 构造。db 为空则退化为进程内存储。
func NewNetworkStore(db *sql.DB) *NetworkStore {
	return &NetworkStore{db: db, nets: map[string]ProjectNetwork{}, bySub: map[string]ProjectNetwork{}}
}

func subnetKey(nodeID, subnet string) string { return nodeID + "|" + subnet }

// Load 从数据库装载全部项目网络（无数据库时为无操作）。
func (s *NetworkStore) Load(ctx context.Context) error {
	if s.db == nil {
		return nil
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id::text, node_id, project_id::text, pool_id::text, subnet_index, subnet, gateway,
		       docker_network_name, status, allocated_at, released_at, reuse_after,
		       COALESCE(last_error,''), created_at, updated_at
		FROM cm_project_networks`)
	if err != nil {
		return fmt.Errorf("load cm_project_networks: %w", err)
	}
	defer rows.Close()
	s.mu.Lock()
	defer s.mu.Unlock()
	for rows.Next() {
		var n ProjectNetwork
		var allocated, released, reuseAfter sql.NullTime
		if err := rows.Scan(&n.ID, &n.NodeID, &n.ProjectID, &n.PoolID, &n.SubnetIndex, &n.Subnet, &n.Gateway,
			&n.DockerNetworkName, &n.Status, &allocated, &released, &reuseAfter,
			&n.LastError, &n.CreatedAt, &n.UpdatedAt); err != nil {
			return fmt.Errorf("scan cm_project_network: %w", err)
		}
		n.AllocatedAt, n.ReleasedAt, n.ReuseAfter = allocated.Time, released.Time, reuseAfter.Time
		s.nets[n.ProjectID] = n
		s.bySub[subnetKey(n.NodeID, n.Subnet)] = n
	}
	return rows.Err()
}

// GetByProject 取某项目的网段记录。
func (s *NetworkStore) GetByProject(projectID string) (ProjectNetwork, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	n, ok := s.nets[projectID]
	return n, ok
}

// InUseOnNode 报告某节点上该子网是否被占用（active/reserved/creating/deleting/failed 均算占用；
// released 且已过冷却期不算）。
func (s *NetworkStore) InUseOnNode(nodeID, subnet string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	n, ok := s.bySub[subnetKey(nodeID, subnet)]
	if !ok {
		return false
	}
	if n.Status == NetReleased && !n.ReuseAfter.After(time.Now()) {
		return false
	}
	return true
}

// IsFree 报告某节点上该子网当前是否可分配：无记录即可用；released 且已过冷却期、
// 且池允许复用，也可用；其余状态（含冷却中的 released）一律占用。
func (s *NetworkStore) IsFree(nodeID, subnet string, reuseEnabled bool) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	n, ok := s.bySub[subnetKey(nodeID, subnet)]
	if !ok {
		return true
	}
	if n.Status == NetReleased {
		return reuseEnabled && !n.ReuseAfter.After(time.Now())
	}
	return false
}

// Put 写入/覆盖一条项目网络记录并落库。
func (s *NetworkStore) Put(ctx context.Context, n ProjectNetwork) (ProjectNetwork, error) {
	if n.ID == "" {
		n.ID = uuid.NewString()
	}
	now := time.Now()
	if n.CreatedAt.IsZero() {
		n.CreatedAt = now
	}
	n.UpdatedAt = now
	if err := s.persist(ctx, n); err != nil {
		return ProjectNetwork{}, err
	}
	s.mu.Lock()
	s.nets[n.ProjectID] = n
	s.bySub[subnetKey(n.NodeID, n.Subnet)] = n
	s.mu.Unlock()
	return n, nil
}

// Adopt 把事务内已落库的记录写入内存视图（不再落库）。
func (s *NetworkStore) Adopt(n ProjectNetwork) {
	s.mu.Lock()
	s.nets[n.ProjectID] = n
	s.bySub[subnetKey(n.NodeID, n.Subnet)] = n
	s.mu.Unlock()
}

// SetStatus 更新项目网络状态（含错误与时间点）。
func (s *NetworkStore) SetStatus(ctx context.Context, projectID, status string, mutate func(*ProjectNetwork)) error {
	s.mu.Lock()
	n, ok := s.nets[projectID]
	if !ok {
		s.mu.Unlock()
		return fmt.Errorf("项目网络不存在: %s", projectID)
	}
	n.Status = status
	n.UpdatedAt = time.Now()
	if mutate != nil {
		mutate(&n)
	}
	s.nets[projectID] = n
	s.bySub[subnetKey(n.NodeID, n.Subnet)] = n
	s.mu.Unlock()
	return s.persist(ctx, n)
}

// Delete 删除一条记录（仅在彻底清理时用；正常释放走 SetStatus=released）。
func (s *NetworkStore) Delete(ctx context.Context, projectID string) error {
	s.mu.Lock()
	n, ok := s.nets[projectID]
	if ok {
		delete(s.nets, projectID)
		delete(s.bySub, subnetKey(n.NodeID, n.Subnet))
	}
	s.mu.Unlock()
	if !ok || s.db == nil {
		return nil
	}
	if _, err := s.db.ExecContext(ctx, `DELETE FROM cm_project_networks WHERE project_id=$1::uuid`, projectID); err != nil {
		return fmt.Errorf("删除项目网络: %w", err)
	}
	return nil
}

// CountByPoolStatus 统计某池下指定状态的记录数（供在用判定与用量统计）。
func (s *NetworkStore) CountByPoolStatus(poolID, status string) int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	n := 0
	for _, pn := range s.nets {
		if pn.PoolID == poolID && pn.Status == status {
			n++
		}
	}
	return n
}

// PoolUsage 汇总某池的占用情况：已分配、已预占、等待复用、可用。
func (s *NetworkStore) PoolUsage(pool Pool) (allocated, reserved, waiting, available int64, err error) {
	capacity, err := pool.CapacityInt64()
	if err != nil {
		return 0, 0, 0, 0, err
	}
	now := time.Now()
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, pn := range s.nets {
		if pn.PoolID != pool.ID {
			continue
		}
		switch pn.Status {
		case NetActive:
			allocated++
		case NetReserved, NetCreating, NetDeleting, NetFailed, NetConflict, NetDrift:
			reserved++
		case NetReleased:
			if pn.ReuseAfter.After(now) {
				waiting++
			}
		}
	}
	available = capacity - allocated - reserved - waiting
	return allocated, reserved, waiting, available, nil
}

func (s *NetworkStore) persist(ctx context.Context, n ProjectNetwork) error {
	if s.db == nil {
		return nil
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO cm_project_networks (id, node_id, project_id, pool_id, subnet_index, subnet, gateway,
			docker_network_name, status, allocated_at, released_at, reuse_after, last_error, created_at, updated_at)
		VALUES ($1::uuid, $2, $3::uuid, $4::uuid, $5, $6, $7, $8, $9,
			$10, $11, $12, $13, $14, $15)
		ON CONFLICT (id) DO UPDATE SET
			subnet_index=EXCLUDED.subnet_index, subnet=EXCLUDED.subnet, gateway=EXCLUDED.gateway,
			docker_network_name=EXCLUDED.docker_network_name, status=EXCLUDED.status,
			allocated_at=EXCLUDED.allocated_at, released_at=EXCLUDED.released_at,
			reuse_after=EXCLUDED.reuse_after, last_error=EXCLUDED.last_error, updated_at=EXCLUDED.updated_at`,
		n.ID, n.NodeID, n.ProjectID, n.PoolID, n.SubnetIndex, n.Subnet, n.Gateway,
		n.DockerNetworkName, n.Status, nullTime(n.AllocatedAt), nullTime(n.ReleasedAt), nullTime(n.ReuseAfter),
		n.LastError, n.CreatedAt, n.UpdatedAt)
	if err != nil {
		return fmt.Errorf("保存项目网络: %w", err)
	}
	return nil
}
