package canary

import (
	"context"
	"fmt"
	"time"

	"entgo.io/ent/dialect/sql"
	"github.com/NeoPlayful/maple-gateway/server/ent"
	entcanary "github.com/NeoPlayful/maple-gateway/server/ent/canaryrelease"
	"github.com/NeoPlayful/maple-gateway/server/pkg"
	"github.com/google/uuid"
)

// Repository 是 Canary 发布数据访问层。
// 普通 CRUD 与状态机 Transition 均走 Ent；Transition 以事务内乐观条件更新防并发。
type Repository struct {
	ent *ent.Client
}

// NewRepository 构造。
func NewRepository(client *ent.Client) *Repository {
	return &Repository{ent: client}
}

func toModel(e *ent.CanaryRelease) *Release {
	return &Release{
		ID:              e.ID,
		ServiceID:       e.ServiceID,
		Name:            e.Name,
		StableVersionID: e.StableVersionID,
		CanaryVersionID: e.CanaryVersionID,
		Phase:           Phase(e.Phase),
		CanaryWeight:    e.CanaryWeight,
		TargetWeight:    e.TargetWeight,
		StepWeight:      e.StepWeight,
		StartedAt:       e.StartedAt,
		FinishedAt:      e.FinishedAt,
		CreatedAt:       e.CreatedAt,
		UpdatedAt:       e.UpdatedAt,
	}
}

// Create 创建发布（初始 phase=created, canary_weight=0）。service 内重名冲突。
func (r *Repository) Create(ctx context.Context, in NewRelease) (*Release, error) {
	target := in.TargetWeight
	if target == 0 {
		target = 100
	}
	step := in.StepWeight
	if step == 0 {
		step = 10
	}
	now := time.Now()
	e, err := r.ent.CanaryRelease.Create().
		SetServiceID(in.ServiceID).
		SetName(in.Name).
		SetStableVersionID(in.StableVersionID).
		SetCanaryVersionID(in.CanaryVersionID).
		SetPhase(string(PhaseCreated)).
		SetCanaryWeight(0).
		SetTargetWeight(target).
		SetStepWeight(step).
		SetCreatedAt(now).
		SetUpdatedAt(now).
		Save(ctx)
	if err != nil {
		if pkg.IsUniqueViolation(err) {
			return nil, pkg.ErrConflict("该服务下已存在同名发布")
		}
		if pkg.IsForeignKeyViolation(err) {
			return nil, pkg.ErrValidation("service_id 或版本 ID 不存在")
		}
		return nil, fmt.Errorf("insert canary release: %w", err)
	}
	return toModel(e), nil
}

// Get 查询发布。
func (r *Repository) Get(ctx context.Context, id uuid.UUID) (*Release, error) {
	e, err := r.ent.CanaryRelease.Get(ctx, id)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, pkg.ErrNotFound("发布不存在")
		}
		return nil, fmt.Errorf("get canary release: %w", err)
	}
	return toModel(e), nil
}

// List 分页列出发布，支持 service_id 筛选与 phase 筛选。
func (r *Repository) List(ctx context.Context, serviceID *uuid.UUID, phase Phase, limit, offset int) ([]*Release, int, error) {
	q := r.ent.CanaryRelease.Query()
	if serviceID != nil {
		q = q.Where(entcanary.ServiceID(*serviceID))
	}
	if phase != "" {
		q = q.Where(entcanary.PhaseEQ(string(phase)))
	}
	total, err := q.Count(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("count canary releases: %w", err)
	}
	es, err := q.
		Order(entcanary.ByCreatedAt(sql.OrderDesc())).
		Limit(limit).
		Offset(offset).
		All(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("list canary releases: %w", err)
	}
	out := make([]*Release, 0, len(es))
	for _, e := range es {
		out = append(out, toModel(e))
	}
	return out, total, nil
}

// Update 应用非空更新（name/target_weight/step_weight），不改 phase 与实时权重。
func (r *Repository) Update(ctx context.Context, id uuid.UUID, in UpdateRelease) (*Release, error) {
	if _, err := r.ent.CanaryRelease.Get(ctx, id); err != nil {
		if ent.IsNotFound(err) {
			return nil, pkg.ErrNotFound("发布不存在")
		}
		return nil, fmt.Errorf("get canary release for update: %w", err)
	}
	upd := r.ent.CanaryRelease.UpdateOneID(id).SetUpdatedAt(time.Now())
	if in.Name != nil {
		upd = upd.SetName(*in.Name)
	}
	if in.TargetWeight != nil {
		upd = upd.SetTargetWeight(*in.TargetWeight)
	}
	if in.StepWeight != nil {
		upd = upd.SetStepWeight(*in.StepWeight)
	}
	e, err := upd.Save(ctx)
	if err != nil {
		if pkg.IsUniqueViolation(err) {
			return nil, pkg.ErrConflict("该服务下已存在同名发布")
		}
		return nil, fmt.Errorf("update canary release: %w", err)
	}
	return toModel(e), nil
}

// Delete 删除发布（不改变版本权重；调用方需确认已结束）。
func (r *Repository) Delete(ctx context.Context, id uuid.UUID) error {
	err := r.ent.CanaryRelease.DeleteOneID(id).Exec(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return pkg.ErrNotFound("发布不存在")
		}
		return fmt.Errorf("delete canary release: %w", err)
	}
	return nil
}

// ---------- 事务内状态机原子操作（ent.Tx + 乐观条件更新防并发） ----------

// Transition 开启事务执行状态机动作：读发布 → fn(txCtx 内 client, rel) → 提交。
// fn 通过事务绑定的 client 操作版本/发布/事件；返回错误则整单回滚。
// 并发安全：updateReleaseState 以事务读到的旧 phase 做乐观 WHERE，0 行视为竞态冲突。
func (r *Repository) Transition(ctx context.Context, id uuid.UUID,
	fn func(ctx context.Context, tc *ent.Client, rel *Release) error) (*Release, error) {
	tx, err := r.ent.Tx(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	tc := tx.Client()

	rel, err := toModelIfFound(tc.CanaryRelease.Get(ctx, id))
	if err != nil {
		return nil, err
	}
	if err := fn(ctx, tc, rel); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return r.Get(ctx, id)
}

func toModelIfFound(e *ent.CanaryRelease, err error) (*Release, error) {
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, pkg.ErrNotFound("发布不存在")
		}
		return nil, err
	}
	return toModel(e), nil
}

// versionRef 事务内读取版本（weight/status/version 名），用于校验与快照。
func versionRef(ctx context.Context, c *ent.Client, id uuid.UUID) (weight int, status string, version string, deploymentID uuid.UUID, err error) {
	v, err := c.DeploymentVersion.Get(ctx, id)
	if err != nil {
		if ent.IsNotFound(err) {
			return 0, "", "", uuid.Nil, pkg.ErrNotFound("版本不存在")
		}
		return 0, "", "", uuid.Nil, err
	}
	return v.Weight, v.Status, v.Version, v.DeploymentID, nil
}

// setVersionWeight 事务内改版本权重。
func setVersionWeight(ctx context.Context, c *ent.Client, id uuid.UUID, weight int) error {
	if err := c.DeploymentVersion.UpdateOneID(id).
		SetWeight(weight).
		SetUpdatedAt(time.Now()).
		Exec(ctx); err != nil {
		return fmt.Errorf("set version weight: %w", err)
	}
	return nil
}

// setVersionStatus 事务内改版本状态（stable/canary/standby/draining/inactive）。
func setVersionStatus(ctx context.Context, c *ent.Client, id uuid.UUID, status string) error {
	if err := c.DeploymentVersion.UpdateOneID(id).
		SetStatus(status).
		SetUpdatedAt(time.Now()).
		Exec(ctx); err != nil {
		return fmt.Errorf("set version status: %w", err)
	}
	return nil
}

// updateReleaseState 事务内改发布 phase/权重/时间，并返回更新后的值。
// 乐观条件：WHERE phase=rel.Phase，防止基于过期状态并发推进（等价于行锁效果）。
func updateReleaseState(ctx context.Context, c *ent.Client, rel *Release, phase Phase,
	canaryWeight int, start, finish bool) (*Release, error) {
	upd := c.CanaryRelease.Update().
		Where(
			entcanary.ID(rel.ID),
			entcanary.PhaseEQ(string(rel.Phase)),
		).
		SetPhase(string(phase)).
		SetCanaryWeight(canaryWeight).
		SetUpdatedAt(time.Now())
	if start {
		if rel.StartedAt == nil {
			upd = upd.SetStartedAt(time.Now())
		}
	}
	if finish {
		upd = upd.SetFinishedAt(time.Now())
	}
	n, err := upd.Save(ctx)
	if err != nil {
		return nil, fmt.Errorf("update canary release state: %w", err)
	}
	if n == 0 {
		return nil, pkg.ErrConflict("发布状态已被并发变更，请重试")
	}
	e, err := c.CanaryRelease.Get(ctx, rel.ID)
	if err != nil {
		return nil, err
	}
	return toModel(e), nil
}

// insertEvent 事务内写事件流水。
func insertEvent(ctx context.Context, c *ent.Client, releaseID uuid.UUID, action Action,
	from, to *int, detail string) error {
	if err := c.CanaryEvent.Create().
		SetReleaseID(releaseID).
		SetPhase(string(action)).
		SetNillableFromWeight(from).
		SetNillableToWeight(to).
		SetDetail(detail).
		SetCreatedAt(time.Now()).
		Exec(ctx); err != nil {
		return fmt.Errorf("insert canary event: %w", err)
	}
	return nil
}
