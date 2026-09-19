package netpools

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/NeoPlayful/maple-gateway/server/internal/containermanager/ipam"
	"github.com/google/uuid"
)

// 分配错误码（对齐文档 §76）。
const (
	CodeAllPoolsExhausted = "IPAM_ALL_POOLS_EXHAUSTED"
	CodePoolExhausted     = "IPAM_POOL_EXHAUSTED"
)

// AllocError 是携带错误码的分配错误。
type AllocError struct {
	Code    string
	Message string
}

func (e *AllocError) Error() string { return e.Message }

// errPoolExhausted 是内部信号：当前池无可用子网，触发切换到下一个池。
var errPoolExhausted = &AllocError{Code: CodePoolExhausted, Message: "网络池无可用子网"}

// nowFn 便于测试注入时间；默认取当前时间。
var nowFn = time.Now

// Allocator 按节点顺序分配子网：取 active 池、priority ASC、顺序分配、耗尽自动切换。
//
// 并发安全：有数据库时以 SELECT ... FOR UPDATE 锁定池行预占，并靠 UNIQUE(node_id,subnet)
// 兜底跨进程冲突（支持多 CM 实例）；无数据库时退化为进程内互斥（仅开发）。
type Allocator struct {
	pools *PoolStore
	nets  *NetworkStore
	db    *sql.DB

	mu sync.Mutex
}

// NewAllocator 构造。
func NewAllocator(pools *PoolStore, nets *NetworkStore, db *sql.DB) *Allocator {
	return &Allocator{pools: pools, nets: nets, db: db}
}

// EnsureSubnet 是 Allocate 的别名，语义更贴合「确保已有网段可用或新分配」。
func (a *Allocator) EnsureSubnet(ctx context.Context, nodeID, projectID, networkName string) (ProjectNetwork, error) {
	return a.Allocate(ctx, nodeID, projectID, networkName)
}

// Allocate 为项目分配一个子网并预占（状态 reserved）。networkName 是 Docker 网络名。
//
// 幂等：项目已有未释放的网段时直接返回，不重新申请——Redeploy/Rollback 复用原网段（文档 §50）。
func (a *Allocator) Allocate(ctx context.Context, nodeID, projectID, networkName string) (ProjectNetwork, error) {
	if nodeID == "" || projectID == "" {
		return ProjectNetwork{}, fmt.Errorf("缺少节点或项目标识")
	}
	if n, ok := a.nets.GetByProject(projectID); ok && n.Status != NetReleased {
		return n, nil
	}

	for _, pool := range a.pools.ListByNode(nodeID) {
		// draining/disabled/conflict 一律不参与分配；exhausted 仍尝试——已释放的网段
		// 可能让它重新有了可用槽位，成功即自我修复回 active。
		if pool.Status == StatusDraining || pool.Status == StatusDisabled || pool.Status == StatusConflict {
			continue
		}
		n, err := a.allocateInPool(ctx, pool, projectID, networkName)
		if err == nil {
			if pool.Status == StatusExhausted {
				_ = a.pools.SetStatus(ctx, pool.ID, StatusActive, "")
			}
			return n, nil
		}
		if errors.Is(err, errPoolExhausted) {
			if pool.Status != StatusExhausted {
				_ = a.pools.SetStatus(ctx, pool.ID, StatusExhausted, "")
			}
			continue
		}
		return ProjectNetwork{}, err
	}
	return ProjectNetwork{}, &AllocError{Code: CodeAllPoolsExhausted, Message: "全部网络池已耗尽，无法分配子网"}
}

// allocateInPool 在单个池内分配一个子网。
func (a *Allocator) allocateInPool(ctx context.Context, pool Pool, projectID, networkName string) (ProjectNetwork, error) {
	poolCIDR, err := pool.CIDR()
	if err != nil {
		return ProjectNetwork{}, err
	}
	capacity, err := pool.CapacityInt64()
	if err != nil {
		return ProjectNetwork{}, err
	}

	if a.db == nil {
		a.mu.Lock()
		defer a.mu.Unlock()
		idx, sub, err := pickSubnet(poolCIDR, capacity, pool.NextIndex, pool.ProjectPrefix, func(s string) bool {
			return a.nets.IsFree(pool.NodeID, s, pool.ReuseEnabled)
		})
		if err != nil {
			return ProjectNetwork{}, err
		}
		n := a.buildNetwork(pool, projectID, networkName, idx, sub)
		return a.nets.Put(ctx, n)
	}

	tx, err := a.db.BeginTx(ctx, nil)
	if err != nil {
		return ProjectNetwork{}, fmt.Errorf("开启分配事务: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// 锁定池行使并发分配串行化（文档 §21）。
	var nextIndex int64
	if err := tx.QueryRowContext(ctx,
		`SELECT next_index FROM cm_node_network_pools WHERE id=$1::uuid FOR UPDATE`, pool.ID).Scan(&nextIndex); err != nil {
		return ProjectNetwork{}, fmt.Errorf("锁定网络池: %w", err)
	}
	used, err := loadUsed(ctx, tx, pool.NodeID, pool.ID)
	if err != nil {
		return ProjectNetwork{}, err
	}
	now := time.Now()
	idx, sub, err := pickSubnet(poolCIDR, capacity, nextIndex, pool.ProjectPrefix, func(s string) bool {
		rec, ok := used[s]
		if !ok {
			return true
		}
		if rec.status == NetReleased {
			return pool.ReuseEnabled && !rec.reuseAfter.After(now)
		}
		return false
	})
	if err != nil {
		return ProjectNetwork{}, err
	}

	n := a.buildNetwork(pool, projectID, networkName, idx, sub)
	if err := insertNetwork(ctx, tx, n); err != nil {
		return ProjectNetwork{}, err
	}
	// 游标前进到已分配序号之后，下次从下一段开始。
	if _, err := tx.ExecContext(ctx,
		`UPDATE cm_node_network_pools SET next_index=$2, updated_at=now() WHERE id=$1::uuid`,
		pool.ID, (idx+1)%capacity); err != nil {
		return ProjectNetwork{}, fmt.Errorf("更新池游标: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return ProjectNetwork{}, fmt.Errorf("提交分配事务: %w", err)
	}

	// 事务已落库，这里只同步内存视图。
	pool.NextIndex = (idx + 1) % capacity
	a.pools.Adopt(pool)
	a.nets.Adopt(n)
	return n, nil
}

// buildNetwork 由序号与子网构造一条 reserved 记录。
func (a *Allocator) buildNetwork(pool Pool, projectID, networkName string, idx int64, sub ipam.CIDR) ProjectNetwork {
	gateway, _ := sub.Gateway()
	now := time.Now()
	return ProjectNetwork{
		ID:                uuid.NewString(),
		NodeID:            pool.NodeID,
		ProjectID:         projectID,
		PoolID:            pool.ID,
		SubnetIndex:       idx,
		Subnet:            sub.String(),
		Gateway:           gateway.String(),
		DockerNetworkName: networkName,
		Status:            NetReserved,
		CreatedAt:         now,
		UpdatedAt:         now,
	}
}

// Release 释放项目的网段：状态置 released，写入 released_at 与 reuse_after 冷却期（文档 §23）。
func (a *Allocator) Release(ctx context.Context, projectID string, delaySeconds int) error {
	n, ok := a.nets.GetByProject(projectID)
	if !ok {
		return nil
	}
	if delaySeconds <= 0 {
		delaySeconds = 600
	}
	now := time.Now()
	if err := a.nets.SetStatus(ctx, projectID, NetReleased, func(pn *ProjectNetwork) {
		pn.ReleasedAt = now
		pn.ReuseAfter = now.Add(time.Duration(delaySeconds) * time.Second)
	}); err != nil {
		return err
	}
	// 释放使池重新有了可用槽位：若该池此前已被标记耗尽，翻回 active 以便再次分配。
	if pool, ok := a.pools.Get(n.PoolID); ok && pool.Status == StatusExhausted {
		_ = a.pools.SetStatus(ctx, pool.ID, StatusActive, "")
	}
	return nil
}

// pickSubnet 从 start 起顺序扫描（回绕），返回首个满足 isFree 的序号与子网；全占满返回 exhausted。
func pickSubnet(poolCIDR ipam.CIDR, capacity, start int64, prefix int, isFree func(subnet string) bool) (int64, ipam.CIDR, error) {
	if capacity <= 0 {
		return 0, ipam.CIDR{}, errPoolExhausted
	}
	if start < 0 || start >= capacity {
		start = 0
	}
	for i := int64(0); i < capacity; i++ {
		idx := (start + i) % capacity
		sub, err := poolCIDR.SubnetAt(idx, prefix)
		if err != nil {
			return 0, ipam.CIDR{}, err
		}
		if isFree(sub.String()) {
			return idx, sub, nil
		}
	}
	return 0, ipam.CIDR{}, errPoolExhausted
}

// usedRec 是已占子网的摘要。
type usedRec struct {
	status     string
	reuseAfter time.Time
}

// loadUsed 读取池内已占子网（含状态与冷却时间）。
func loadUsed(ctx context.Context, tx *sql.Tx, nodeID, poolID string) (map[string]usedRec, error) {
	rows, err := tx.QueryContext(ctx,
		`SELECT subnet, status, reuse_after FROM cm_project_networks WHERE node_id=$1 AND pool_id=$2::uuid`,
		nodeID, poolID)
	if err != nil {
		return nil, fmt.Errorf("读取池内占用: %w", err)
	}
	defer rows.Close()
	out := map[string]usedRec{}
	for rows.Next() {
		var subnet, status string
		var reuseAfter sql.NullTime
		if err := rows.Scan(&subnet, &status, &reuseAfter); err != nil {
			return nil, fmt.Errorf("扫描池内占用: %w", err)
		}
		out[subnet] = usedRec{status: status, reuseAfter: reuseAfter.Time}
	}
	return out, rows.Err()
}

// insertNetwork 在事务内写入一条项目网络记录。
func insertNetwork(ctx context.Context, tx *sql.Tx, n ProjectNetwork) error {
	_, err := tx.ExecContext(ctx, `
		INSERT INTO cm_project_networks (id, node_id, project_id, pool_id, subnet_index, subnet, gateway,
			docker_network_name, status, allocated_at, released_at, reuse_after, last_error, created_at, updated_at)
		VALUES ($1::uuid, $2, $3::uuid, $4::uuid, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)`,
		n.ID, n.NodeID, n.ProjectID, n.PoolID, n.SubnetIndex, n.Subnet, n.Gateway,
		n.DockerNetworkName, n.Status, nullTime(n.AllocatedAt), nullTime(n.ReleasedAt), nullTime(n.ReuseAfter),
		n.LastError, n.CreatedAt, n.UpdatedAt)
	if err != nil {
		// 唯一约束冲突（并发抢占同一子网）按需重试由上层处理，这里如实上报。
		return fmt.Errorf("预占子网: %w", err)
	}
	return nil
}
