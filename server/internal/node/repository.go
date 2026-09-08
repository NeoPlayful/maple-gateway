package node

import (
	"context"
	"fmt"
	"time"

	"entgo.io/ent/dialect/sql"
	"github.com/NeoPlayful/maple-gateway/server/ent"
	entinstance "github.com/NeoPlayful/maple-gateway/server/ent/instance"
	entnode "github.com/NeoPlayful/maple-gateway/server/ent/node"
	"github.com/NeoPlayful/maple-gateway/server/pkg"
	"github.com/google/uuid"
)

// Repository 是 Node 数据访问层（基于 Ent）。
type Repository struct {
	ent *ent.Client
}

// NewRepository 构造。
func NewRepository(client *ent.Client) *Repository {
	return &Repository{ent: client}
}

// toModel 把 Ent 实体映射为领域模型。
func toModel(e *ent.Node) *Node {
	return &Node{
		ID:         e.ID,
		Name:       e.Name,
		Host:       e.Host,
		Region:     e.Region,
		Labels:     e.Labels,
		Status:     Status(e.Status),
		Weight:     e.Weight,
		LastSeenAt: e.LastSeenAt,
		CreatedAt:  e.CreatedAt,
		UpdatedAt:  e.UpdatedAt,
	}
}

// Create 注册节点。name 冲突返回 Conflict。
func (r *Repository) Create(ctx context.Context, in New) (*Node, error) {
	weight := in.Weight
	if weight == 0 {
		weight = 1
	}
	now := time.Now()
	e, err := r.ent.Node.Create().
		SetName(in.Name).
		SetHost(in.Host).
		SetRegion(in.Region).
		SetLabels(in.Labels).
		SetStatus(string(StatusOnline)).
		SetWeight(weight).
		SetCreatedAt(now).
		SetUpdatedAt(now).
		Save(ctx)
	if err != nil {
		if pkg.IsUniqueViolation(err) {
			return nil, pkg.ErrConflict("节点名已存在")
		}
		return nil, fmt.Errorf("insert node: %w", err)
	}
	return toModel(e), nil
}

// GetByID 查询单个节点。
func (r *Repository) GetByID(ctx context.Context, id uuid.UUID) (*Node, error) {
	e, err := r.ent.Node.Get(ctx, id)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, pkg.ErrNotFound("节点不存在")
		}
		return nil, fmt.Errorf("get node: %w", err)
	}
	return toModel(e), nil
}

// RoutableMap 返回 node_id → 是否可接收流量（online 才可路由）。
// 供路由表重建过滤 offline/maintenance/disabled 节点上的实例。
func (r *Repository) RoutableMap(ctx context.Context) (map[uuid.UUID]bool, error) {
	ns, err := r.ent.Node.Query().Select(entnode.FieldID, entnode.FieldStatus).All(ctx)
	if err != nil {
		return nil, fmt.Errorf("node routable map: %w", err)
	}
	out := make(map[uuid.UUID]bool, len(ns))
	for _, n := range ns {
		out[n.ID] = n.Status == string(StatusOnline)
	}
	return out, nil
}

// All 返回全部节点（路由缓存构建 / 心跳扫描用）。
func (r *Repository) All(ctx context.Context) ([]*Node, error) {
	es, err := r.ent.Node.Query().Order(entnode.ByCreatedAt()).All(ctx)
	if err != nil {
		return nil, fmt.Errorf("all nodes: %w", err)
	}
	out := make([]*Node, 0, len(es))
	for _, e := range es {
		out = append(out, toModel(e))
	}
	return out, nil
}

// List 分页列出节点，支持按状态筛选。
func (r *Repository) List(ctx context.Context, status Status, limit, offset int) ([]*Node, int, error) {
	q := r.ent.Node.Query()
	if status != "" {
		q = q.Where(entnode.StatusEQ(string(status)))
	}
	total, err := q.Count(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("count nodes: %w", err)
	}
	es, err := q.
		Order(entnode.ByCreatedAt(sql.OrderDesc())).
		Limit(limit).
		Offset(offset).
		All(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("list nodes: %w", err)
	}
	out := make([]*Node, 0, len(es))
	for _, e := range es {
		out = append(out, toModel(e))
	}
	return out, total, nil
}

// Update 应用非空更新（host/region/labels/weight/status）。
func (r *Repository) Update(ctx context.Context, id uuid.UUID, in Update) (*Node, error) {
	if _, err := r.ent.Node.Get(ctx, id); err != nil {
		if ent.IsNotFound(err) {
			return nil, pkg.ErrNotFound("节点不存在")
		}
		return nil, fmt.Errorf("get node for update: %w", err)
	}
	upd := r.ent.Node.UpdateOneID(id).SetUpdatedAt(time.Now())
	if in.Host != nil {
		upd = upd.SetHost(*in.Host)
	}
	if in.Region != nil {
		upd = upd.SetRegion(*in.Region)
	}
	if in.Labels != nil {
		upd = upd.SetLabels(in.Labels)
	}
	if in.Weight != nil {
		upd = upd.SetWeight(*in.Weight)
	}
	if in.Status != nil {
		upd = upd.SetStatus(string(*in.Status))
	}
	e, err := upd.Save(ctx)
	if err != nil {
		return nil, fmt.Errorf("update node: %w", err)
	}
	return toModel(e), nil
}

// SetStatus 便捷状态变更（enable/disable/maintenance）。
func (r *Repository) SetStatus(ctx context.Context, id uuid.UUID, s Status) (*Node, error) {
	return r.Update(ctx, id, Update{Status: &s})
}

// Heartbeat 刷新节点最后心跳时间；非 disabled 节点心跳即恢复 online。
func (r *Repository) Heartbeat(ctx context.Context, id uuid.UUID) (*Node, error) {
	// 需要"disabled 保持不变，否则 online"的条件赋值：先读当前状态再决定。
	cur, err := r.ent.Node.Get(ctx, id)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, pkg.ErrNotFound("节点不存在")
		}
		return nil, fmt.Errorf("get node for heartbeat: %w", err)
	}
	next := string(StatusOnline)
	if cur.Status == string(StatusDisabled) {
		next = string(StatusDisabled)
	}
	now := time.Now()
	e, err := r.ent.Node.UpdateOneID(id).
		SetStatus(next).
		SetLastSeenAt(now).
		SetUpdatedAt(now).
		Save(ctx)
	if err != nil {
		return nil, fmt.Errorf("heartbeat node: %w", err)
	}
	return toModel(e), nil
}

// MarkOffline 把 last_seen_at 早于 cutoff（心跳超时）且当前非 disabled 的节点置为 offline。
// 返回被标记的节点数（供 watchdog 日志）。disabled 节点保持人工状态，不被覆盖。
func (r *Repository) MarkOffline(ctx context.Context, cutoff time.Time) (int64, error) {
	n, err := r.ent.Node.Update().
		Where(
			entnode.StatusNEQ(string(StatusDisabled)),
			entnode.Or(
				entnode.LastSeenAtIsNil(),
				entnode.LastSeenAtLT(cutoff),
			),
		).
		SetStatus(string(StatusOffline)).
		SetUpdatedAt(time.Now()).
		Save(ctx)
	if err != nil {
		return 0, fmt.Errorf("mark nodes offline: %w", err)
	}
	return int64(n), nil
}

// Delete 删除节点，并清空其上实例的 node_id 引用（同事务，保持原子性）。
func (r *Repository) Delete(ctx context.Context, id uuid.UUID) error {
	tx, err := r.ent.Tx(ctx)
	if err != nil {
		return fmt.Errorf("begin delete node tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.Instance.Update().
		Where(entinstance.NodeID(id)).
		ClearNodeID().
		Save(ctx); err != nil {
		return fmt.Errorf("detach instances from node: %w", err)
	}
	err = tx.Node.DeleteOneID(id).Exec(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return pkg.ErrNotFound("节点不存在")
		}
		return fmt.Errorf("delete node: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit delete node: %w", err)
	}
	return nil
}
