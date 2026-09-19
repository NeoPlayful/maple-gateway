package netpools

import (
	"context"
	"fmt"

	"github.com/NeoPlayful/maple-gateway/server/internal/containermanager/ipam"
)

// PoolView 是网络池对外的视图：池字段 + 容量/用量统计。
type PoolView struct {
	Pool
	Capacity  int64   `json:"capacity"`
	Allocated int64   `json:"allocated"`
	Reserved  int64   `json:"reserved"`
	Waiting   int64   `json:"released_waiting"`
	Available int64   `json:"available"`
	Usage     float64 `json:"usage"`
}

// CreatePoolInput 是新增网络池的入参。
type CreatePoolInput struct {
	NodeID        string `json:"node_id"`
	Name          string `json:"name"`
	Description   string `json:"description"`
	AddressPool   string `json:"address_pool"`
	ProjectPrefix int    `json:"project_prefix"`
	Priority      int    `json:"priority"`
}

// UpdatePoolInput 是可修改字段的入参（address_pool/project_prefix 不可改）。
type UpdatePoolInput struct {
	Name              string `json:"name"`
	Description       string `json:"description"`
	Priority          *int   `json:"priority"`
	Status            string `json:"status"`
	ReuseEnabled      *bool  `json:"reuse_enabled"`
	ReuseDelaySeconds *int   `json:"reuse_delay_seconds"`
}

// Service 是网络池的管理门面：CRUD + 状态流转 + 冲突复检。
type Service struct {
	pools       *PoolStore
	nets        *NetworkStore
	checker     PoolChecker
	allocator   *Allocator
	defaultPool DefaultPoolConfig
}

// PoolChecker 复检池与节点实际网络（Docker 网络/主机路由/接口）的冲突。
type PoolChecker interface {
	Check(ctx context.Context, nodeID string, poolCIDR ipam.CIDR) (conflict bool, reason string, err error)
}

// DefaultPoolConfig 是新节点自动初始化默认池的配置。
type DefaultPoolConfig struct {
	AddressPool       string
	ProjectPrefix     int
	ReuseEnabled      bool
	ReuseDelaySeconds int
}

// DefaultSystemPool 返回系统默认池配置（文档第13节）。
func DefaultSystemPool() DefaultPoolConfig {
	return DefaultPoolConfig{
		AddressPool: "10.128.0.0/9", ProjectPrefix: 24,
		ReuseEnabled: true, ReuseDelaySeconds: 600,
	}
}

// WithDefaultPool 覆盖默认池配置（来自 CM 配置或共享 settings）。
func (s *Service) WithDefaultPool(cfg DefaultPoolConfig) *Service {
	if cfg.AddressPool != "" && cfg.ProjectPrefix > 0 {
		s.defaultPool = cfg
	}
	return s
}

// SetDefaultPool 运行期替换默认池配置（新节点上线时按最新系统默认初始化）。
func (s *Service) SetDefaultPool(cfg DefaultPoolConfig) {
	if cfg.AddressPool != "" && cfg.ProjectPrefix > 0 {
		s.defaultPool = cfg
	}
}

// NewService 构造。checker 可空（未接入 Agent 时跳过冲突复检）。
func NewService(pools *PoolStore, nets *NetworkStore, alloc *Allocator, checker PoolChecker) *Service {
	return &Service{pools: pools, nets: nets, allocator: alloc, checker: checker, defaultPool: DefaultSystemPool()}
}

// List 列出某节点的网络池（带用量统计）。
func (s *Service) List(nodeID string) []PoolView {
	pools := s.pools.ListByNode(nodeID)
	out := make([]PoolView, 0, len(pools))
	for _, p := range pools {
		out = append(out, s.view(p))
	}
	return out
}

// ListAll 列出全部池。
func (s *Service) ListAll() []PoolView {
	pools := s.pools.List()
	out := make([]PoolView, 0, len(pools))
	for _, p := range pools {
		out = append(out, s.view(p))
	}
	return out
}

// Create 新增网络池：校验 CIDR/前缀，做同节点重叠检测与冲突复检。
func (s *Service) Create(ctx context.Context, in CreatePoolInput) (PoolView, error) {
	pool, err := s.pools.Create(ctx, Pool{
		NodeID: in.NodeID, Name: in.Name, Description: in.Description,
		AddressPool: in.AddressPool, ProjectPrefix: in.ProjectPrefix, Priority: in.Priority,
	})
	if err != nil {
		return PoolView{}, err
	}
	view := s.view(pool)
	// 冲突复检：与 Docker 网络/主机路由重叠则标记 conflict（不阻断创建，便于管理员查看）。
	if s.checker != nil {
		if cidr, err := pool.CIDR(); err == nil {
			if conflict, reason, cerr := s.checker.Check(ctx, pool.NodeID, cidr); cerr != nil {
				view.LastConflictReason = cerr.Error()
			} else if conflict {
				_ = s.pools.SetStatus(ctx, pool.ID, StatusConflict, reason)
				view.Status = StatusConflict
				view.LastConflictReason = reason
			}
		}
	}
	return view, nil
}

// Update 更新池可变字段。含在用池 CIDR/前缀不变的护栏（此处 CIDR 本就不在入参中）。
func (s *Service) Update(ctx context.Context, nodeID, poolID string, in UpdatePoolInput) (PoolView, error) {
	old, ok := s.pools.Get(poolID)
	if !ok || old.NodeID != nodeID {
		return PoolView{}, fmt.Errorf("池不存在")
	}
	cur := old
	if in.Name != "" {
		cur.Name = in.Name
	}
	cur.Description = in.Description
	if in.Priority != nil {
		cur.Priority = *in.Priority
	}
	if in.Status != "" {
		cur.Status = in.Status
	}
	if in.ReuseEnabled != nil {
		cur.ReuseEnabled = *in.ReuseEnabled
	}
	if in.ReuseDelaySeconds != nil {
		cur.ReuseDelaySeconds = *in.ReuseDelaySeconds
	}
	out, err := s.pools.Update(ctx, cur)
	if err != nil {
		return PoolView{}, err
	}
	return s.view(out), nil
}

// Delete 删除池；仍被项目占用（active/reserved/deleting/failed）时拒绝（文档 §42/§66）。
func (s *Service) Delete(ctx context.Context, nodeID, poolID string) error {
	pool, ok := s.pools.Get(poolID)
	if !ok || pool.NodeID != nodeID {
		return fmt.Errorf("池不存在")
	}
	for _, st := range []string{NetActive, NetReserved, NetCreating, NetDeleting, NetFailed} {
		if s.nets.CountByPoolStatus(poolID, st) > 0 {
			return &AllocError{Code: "IPAM_POOL_IN_USE", Message: "池仍被项目占用，无法删除"}
		}
	}
	return s.pools.Delete(ctx, poolID)
}

// Check 复检池冲突并更新状态。
func (s *Service) Check(ctx context.Context, nodeID, poolID string) (PoolView, error) {
	pool, ok := s.pools.Get(poolID)
	if !ok || pool.NodeID != nodeID {
		return PoolView{}, fmt.Errorf("池不存在")
	}
	if s.checker == nil {
		return s.view(pool), nil
	}
	cidr, err := pool.CIDR()
	if err != nil {
		return PoolView{}, err
	}
	conflict, reason, err := s.checker.Check(ctx, nodeID, cidr)
	if err != nil {
		return PoolView{}, err
	}
	if conflict {
		if err := s.pools.SetStatus(ctx, poolID, StatusConflict, reason); err != nil {
			return PoolView{}, err
		}
	} else if pool.Status == StatusConflict {
		// 冲突消除则翻回 active。
		if err := s.pools.SetStatus(ctx, poolID, StatusActive, ""); err != nil {
			return PoolView{}, err
		}
	}
	out, _ := s.pools.Get(poolID)
	return s.view(out), nil
}

// EnsureDefaultForNode 为新注册节点自动创建默认池（幂等：已有池则不重复建）。
func (s *Service) EnsureDefaultForNode(ctx context.Context, nodeID string) error {
	if len(s.pools.ListByNode(nodeID)) > 0 {
		return nil
	}
	created, err := s.pools.Create(ctx, Pool{
		NodeID: nodeID, Name: "default",
		AddressPool: s.defaultPool.AddressPool, ProjectPrefix: s.defaultPool.ProjectPrefix,
		Priority: 10, IsSystemDefault: true,
	})
	if err != nil {
		return err
	}
	// Create 统一置复用于启用；此处按系统默认覆盖为管理员设定值（默认仍为启用/600s）。
	created.ReuseEnabled = s.defaultPool.ReuseEnabled
	if s.defaultPool.ReuseDelaySeconds > 0 {
		created.ReuseDelaySeconds = s.defaultPool.ReuseDelaySeconds
	}
	_, err = s.pools.Update(ctx, created)
	return err
}

// view 组装池视图（含用量统计）。
func (s *Service) view(p Pool) PoolView {
	v := PoolView{Pool: p}
	capacity, err := p.CapacityInt64()
	if err != nil {
		return v
	}
	v.Capacity = capacity
	allocated, reserved, waiting, available, err := s.nets.PoolUsage(p)
	if err != nil {
		return v
	}
	v.Allocated, v.Reserved, v.Waiting, v.Available = allocated, reserved, waiting, available
	if capacity > 0 {
		v.Usage = float64(allocated+reserved) / float64(capacity)
	}
	return v
}
