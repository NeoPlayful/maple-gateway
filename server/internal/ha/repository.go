package ha

import (
	"context"
	"fmt"
	"time"

	"entgo.io/ent/dialect/sql"
	"github.com/NeoPlayful/maple-gateway/server/ent"
	entha "github.com/NeoPlayful/maple-gateway/server/ent/gatewayinstance"
	"github.com/NeoPlayful/maple-gateway/server/pkg"
	"github.com/google/uuid"
)

// Repository 是 Gateway 自身实例数据访问层（基于 Ent）。
type Repository struct {
	ent *ent.Client
}

// NewRepository 构造。
func NewRepository(client *ent.Client) *Repository {
	return &Repository{ent: client}
}

// toModel 把 Ent 实体映射为领域模型。
func toModel(e *ent.GatewayInstance) *Instance {
	return &Instance{
		ID:         e.ID,
		InstanceID: e.InstanceID,
		Addr:       e.Addr,
		Hostname:   e.Hostname,
		Status:     Status(e.Status),
		Role:       Role(e.Role),
		LeaseUntil: e.LeaseUntil,
		Version:    e.Version,
		StartedAt:  e.StartedAt,
		LastSeenAt: e.LastSeenAt,
		CreatedAt:  e.CreatedAt,
		UpdatedAt:  e.UpdatedAt,
	}
}

// Upsert 注册/续期本进程实例：存在则原地刷新，不存在则插入（按 instance_id 幂等）。
func (r *Repository) Upsert(ctx context.Context, in Register, status Status, role Role,
	leaseUntil *time.Time) (*Instance, error) {
	now := time.Now()
	id, err := r.ent.GatewayInstance.Create().
		SetInstanceID(in.InstanceID).
		SetAddr(in.Addr).
		SetHostname(in.Hostname).
		SetStatus(string(status)).
		SetRole(string(role)).
		SetNillableLeaseUntil(leaseUntil).
		SetVersion(in.Version).
		SetStartedAt(now).
		SetLastSeenAt(now).
		SetCreatedAt(now).
		SetUpdatedAt(now).
		OnConflictColumns(entha.FieldInstanceID).
		UpdateNewValues().
		ID(ctx)
	if err != nil {
		return nil, fmt.Errorf("upsert gateway instance: %w", err)
	}
	return r.GetByID(ctx, id)
}

// GetByID 查询单个 Gateway 实例。
func (r *Repository) GetByID(ctx context.Context, id uuid.UUID) (*Instance, error) {
	e, err := r.ent.GatewayInstance.Get(ctx, id)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, pkg.ErrNotFound("gateway instance not found")
		}
		return nil, fmt.Errorf("get gateway instance: %w", err)
	}
	return toModel(e), nil
}

// GetByInstanceID 按 instance_id 查询。
func (r *Repository) GetByInstanceID(ctx context.Context, instanceID string) (*Instance, error) {
	e, err := r.ent.GatewayInstance.Query().
		Where(entha.InstanceIDEQ(instanceID)).
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("get gateway instance by instance id: %w", err)
	}
	return toModel(e), nil
}

// List 列出全部 Gateway 实例。
func (r *Repository) List(ctx context.Context) ([]*Instance, error) {
	es, err := r.ent.GatewayInstance.Query().
		Order(entha.ByUpdatedAt(sql.OrderDesc())).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("list gateway instances: %w", err)
	}
	out := make([]*Instance, 0, len(es))
	for _, e := range es {
		out = append(out, toModel(e))
	}
	return out, nil
}

// ListLeaders 列出当前 lease 有效（未过期）的 leader。
func (r *Repository) ListLeaders(ctx context.Context) ([]*Instance, error) {
	now := time.Now()
	es, err := r.ent.GatewayInstance.Query().
		Where(
			entha.RoleEQ(string(RoleLeader)),
			entha.Or(
				entha.LeaseUntilIsNil(),
				entha.LeaseUntilGT(now),
			),
		).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("list gateway leaders: %w", err)
	}
	out := make([]*Instance, 0, len(es))
	for _, e := range es {
		out = append(out, toModel(e))
	}
	return out, nil
}

// Heartbeat 刷新 last_seen_at；可顺带续期 lease / 更新 role。
func (r *Repository) Heartbeat(ctx context.Context, id uuid.UUID, role Role,
	leaseUntil *time.Time) (*Instance, error) {
	now := time.Now()
	upd := r.ent.GatewayInstance.UpdateOneID(id).
		SetStatus(string(StatusOnline)).
		SetRole(string(role)).
		SetLastSeenAt(now).
		SetUpdatedAt(now)
	if leaseUntil != nil {
		upd = upd.SetLeaseUntil(*leaseUntil)
	}
	e, err := upd.Save(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, pkg.ErrNotFound("gateway instance not found")
		}
		return nil, fmt.Errorf("heartbeat gateway instance: %w", err)
	}
	return toModel(e), nil
}

// TryAcquireLeaseDB 以 DB lease 原子抢占/续期 leader 身份：
// 仅当本行 lease 未过期（含从未设置）且当前无有效 leader 时成功。
// 若已被其他实例持有且未过期则失败（返回 ok=false）。
func (r *Repository) TryAcquireLeaseDB(ctx context.Context, id uuid.UUID, role Role,
	leaseUntil time.Time) (bool, error) {
	now := time.Now()
	n, err := r.ent.GatewayInstance.Update().
		Where(
			entha.ID(id),
			entha.StatusEQ(string(StatusOnline)),
			entha.RoleNEQ(string(RoleLeader)), // 已是 leader 走续期分支
			entha.Or(
				entha.LeaseUntilIsNil(),
				entha.LeaseUntilLT(now),
			),
		).
		SetRole(string(role)).
		SetLeaseUntil(leaseUntil).
		SetUpdatedAt(now).
		Save(ctx)
	if err != nil {
		return false, fmt.Errorf("acquire db lease: %w", err)
	}
	return n > 0, nil
}

// TryRenewLeaseDB 续期自己持有的 lease（仅当仍是 leader 且未过期）。
func (r *Repository) TryRenewLeaseDB(ctx context.Context, id uuid.UUID,
	leaseUntil time.Time) (bool, error) {
	now := time.Now()
	n, err := r.ent.GatewayInstance.Update().
		Where(
			entha.ID(id),
			entha.RoleEQ(string(RoleLeader)),
			entha.StatusEQ(string(StatusOnline)),
			entha.Or(
				entha.LeaseUntilIsNil(),
				entha.LeaseUntilGT(now),
			),
		).
		SetLeaseUntil(leaseUntil).
		SetUpdatedAt(now).
		Save(ctx)
	if err != nil {
		return false, fmt.Errorf("renew db lease: %w", err)
	}
	return n > 0, nil
}

// MarkOffline 把心跳超时的实例置 offline（看护用）。
func (r *Repository) MarkOffline(ctx context.Context, cutoff time.Time) (int64, error) {
	n, err := r.ent.GatewayInstance.Update().
		Where(
			entha.StatusNEQ(string(StatusMaintenance)),
			entha.Or(
				entha.LastSeenAtIsNil(),
				entha.LastSeenAtLT(cutoff),
			),
		).
		SetStatus(string(StatusOffline)).
		SetRole(string(RoleFollower)).
		SetUpdatedAt(time.Now()).
		Save(ctx)
	if err != nil {
		return 0, fmt.Errorf("mark gateway instances offline: %w", err)
	}
	return int64(n), nil
}
