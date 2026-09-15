package release

import (
	"context"
	"fmt"
	"time"

	"entgo.io/ent/dialect/sql"
	"github.com/NeoPlayful/maple-gateway/server/ent"
	entrelease "github.com/NeoPlayful/maple-gateway/server/ent/release"
	entreleaseevent "github.com/NeoPlayful/maple-gateway/server/ent/releaseevent"
	"github.com/NeoPlayful/maple-gateway/server/pkg"
	"github.com/google/uuid"
)

// Repository 是统一发布的数据访问层。
// CRUD 与状态机 Transition 均走 Ent；Transition 以事务内乐观条件更新防并发。
type Repository struct {
	ent *ent.Client
}

// NewRepository 构造。
func NewRepository(client *ent.Client) *Repository {
	return &Repository{ent: client}
}

func toModel(e *ent.Release) *Release {
	return &Release{
		ID:                 e.ID,
		Strategy:           Strategy(e.Strategy),
		DeploymentID:       e.DeploymentID,
		ServiceID:          e.ServiceID,
		Name:               e.Name,
		Phase:              Phase(e.Phase),
		PrimaryVersionID:   e.PrimaryVersionID,
		SecondaryVersionID: e.SecondaryVersionID,
		PrimaryWeight:      e.PrimaryWeight,
		SecondaryWeight:    e.SecondaryWeight,
		PreviousPrimaryID:  e.PreviousPrimaryID,
		Config:             e.Config,
		StartedAt:          e.StartedAt,
		FinishedAt:         e.FinishedAt,
		CreatedAt:          e.CreatedAt,
		UpdatedAt:          e.UpdatedAt,
	}
}

// CreateTx 事务内落库（phase/权重/分配由 strategy.createPlan 决定）。
// 走传入的事务 client，供创建动作把"登记 + 初始翻转"放在同一事务。
func (r *Repository) CreateTx(ctx context.Context, c *ent.Client, p *createPlan) (*Release, error) {
	now := time.Now()
	e, err := c.Release.Create().
		SetStrategy(string(p.Strategy)).
		SetDeploymentID(p.DeploymentID).
		SetNillableServiceID(p.ServiceID).
		SetName(p.Name).
		SetPhase(string(p.Phase)).
		SetPrimaryVersionID(p.PrimaryVersionID).
		SetSecondaryVersionID(p.SecondaryVersionID).
		SetPrimaryWeight(p.PrimaryWeight).
		SetSecondaryWeight(p.SecondaryWeight).
		SetNillablePreviousPrimaryID(p.PreviousPrimaryID).
		SetConfig(p.Config).
		SetCreatedAt(now).
		SetUpdatedAt(now).
		Save(ctx)
	if err != nil {
		if pkg.IsUniqueViolation(err) {
			return nil, pkg.ErrConflict("该部署已存在活跃发布（每部署至多一个）")
		}
		if pkg.IsForeignKeyViolation(err) {
			return nil, pkg.ErrValidation("deployment_id 或版本 ID 不存在")
		}
		return nil, fmt.Errorf("insert release: %w", err)
	}
	return toModel(e), nil
}

// Get 查询发布。
func (r *Repository) Get(ctx context.Context, id uuid.UUID) (*Release, error) {
	e, err := r.ent.Release.Get(ctx, id)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, pkg.ErrNotFound("发布不存在")
		}
		return nil, fmt.Errorf("get release: %w", err)
	}
	return toModel(e), nil
}

// List 分页列出；strategy/serviceID/phase 可选过滤。
func (r *Repository) List(ctx context.Context, strategy *Strategy, serviceID *uuid.UUID,
	phase Phase, limit, offset int) ([]*Release, int, error) {
	q := r.ent.Release.Query()
	if strategy != nil {
		q = q.Where(entrelease.StrategyEQ(string(*strategy)))
	}
	if serviceID != nil {
		q = q.Where(entrelease.ServiceIDEQ(*serviceID))
	}
	if phase != "" {
		q = q.Where(entrelease.PhaseEQ(string(phase)))
	}
	total, err := q.Count(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("count releases: %w", err)
	}
	es, err := q.
		Order(entrelease.ByCreatedAt(sql.OrderDesc())).
		Limit(limit).
		Offset(offset).
		All(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("list releases: %w", err)
	}
	out := make([]*Release, 0, len(es))
	for _, e := range es {
		out = append(out, toModel(e))
	}
	return out, total, nil
}

// Update 应用非空更新（name / canary 的 target/step），不改 phase 与实时权重。
func (r *Repository) Update(ctx context.Context, id uuid.UUID, in UpdateRelease) (*Release, error) {
	rel, err := r.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	upd := r.ent.Release.UpdateOneID(id).SetUpdatedAt(time.Now())
	if in.Name != nil {
		upd = upd.SetName(*in.Name)
	}
	// target/step 仅对 canary 有意义，写进 config JSONB。
	if rel.Strategy == StrategyCanary && (in.TargetWeight != nil || in.StepWeight != nil) {
		cfg := parseCanaryConfig(rel.Config)
		if in.TargetWeight != nil {
			cfg.TargetWeight = *in.TargetWeight
		}
		if in.StepWeight != nil {
			cfg.StepWeight = *in.StepWeight
		}
		raw, _ := marshalConfig(cfg)
		upd = upd.SetConfig(raw)
	}
	e, err := upd.Save(ctx)
	if err != nil {
		return nil, fmt.Errorf("update release: %w", err)
	}
	return toModel(e), nil
}

// Events 返回某发布的事件流水（按时间倒序）。
func (r *Repository) Events(ctx context.Context, id uuid.UUID) ([]*Event, error) {
	es, err := r.ent.ReleaseEvent.Query().
		Where(entreleaseevent.ReleaseIDEQ(id)).
		Order(entreleaseevent.ByCreatedAt(sql.OrderDesc())).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("list release events: %w", err)
	}
	out := make([]*Event, 0, len(es))
	for _, e := range es {
		out = append(out, &Event{
			ID:          e.ID,
			ReleaseID:   e.ReleaseID,
			Action:      Action(e.Action),
			FromWeight:  e.FromWeight,
			ToWeight:    e.ToWeight,
			FromVersion: e.FromVersion,
			ToVersion:   e.ToVersion,
			Detail:      e.Detail,
			CreatedAt:   e.CreatedAt,
		})
	}
	return out, nil
}

// Delete 删除发布（调用方需确认已结束；不改变版本权重）。
func (r *Repository) Delete(ctx context.Context, id uuid.UUID) error {
	err := r.ent.Release.DeleteOneID(id).Exec(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return pkg.ErrNotFound("发布不存在")
		}
		return fmt.Errorf("delete release: %w", err)
	}
	return nil
}

// Transition 开启事务执行状态机动作：读发布 → fn(txClient, rel) → 提交。
// 并发安全：updateState 以事务读到的旧 phase 做乐观 WHERE，0 行视为竞态冲突。
func (r *Repository) Transition(ctx context.Context, id uuid.UUID,
	fn func(ctx context.Context, tc *ent.Client, rel *Release) error) (*Release, error) {
	tx, err := r.ent.Tx(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	tc := tx.Client()

	rel, err := toModelIfFound(tc.Release.Get(ctx, id))
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

func toModelIfFound(e *ent.Release, err error) (*Release, error) {
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, pkg.ErrNotFound("发布不存在")
		}
		return nil, err
	}
	return toModel(e), nil
}

// ---------- 事务内 helper（共用效果写入） ----------

// applyAllocation 是统一的"效果写入"：把一组 版本→(weight,status) 在同一事务内落库。
// canary 与 bluegreen 各自算 allocation，共用这条写路径——这是去重的核心。
func applyAllocation(ctx context.Context, c *ent.Client, allocs []allocation) error {
	now := time.Now()
	for _, a := range allocs {
		if err := c.DeploymentVersion.UpdateOneID(a.VersionID).
			SetWeight(a.Weight).
			SetStatus(a.Status).
			SetUpdatedAt(now).
			Exec(ctx); err != nil {
			return fmt.Errorf("apply allocation version=%s: %w", a.VersionID, err)
		}
	}
	return nil
}

// updateState 事务内改发布状态/权重/previous/时间，并返回更新后的值。
// 乐观条件：WHERE phase=rel.Phase，防止基于过期状态并发推进。
func updateState(ctx context.Context, c *ent.Client, rel *Release, res *actionResult,
	previous *uuid.UUID) (*Release, error) {
	upd := c.Release.Update().
		Where(
			entrelease.ID(rel.ID),
			entrelease.PhaseEQ(string(rel.Phase)),
		).
		SetPhase(string(res.Phase)).
		SetPrimaryWeight(res.PrimaryWeight).
		SetSecondaryWeight(res.SecondaryWeight).
		SetUpdatedAt(time.Now())
	if previous != nil {
		upd = upd.SetPreviousPrimaryID(*previous)
	}
	if res.SetStart && rel.StartedAt == nil {
		upd = upd.SetStartedAt(time.Now())
	}
	if res.SetFinish {
		upd = upd.SetFinishedAt(time.Now())
	}
	n, err := upd.Save(ctx)
	if err != nil {
		return nil, fmt.Errorf("update release state: %w", err)
	}
	if n == 0 {
		return nil, pkg.ErrConflict("发布状态已被并发变更，请重试")
	}
	e, err := c.Release.Get(ctx, rel.ID)
	if err != nil {
		return nil, err
	}
	return toModel(e), nil
}

// insertEvent 事务内写事件流水。
func insertEvent(ctx context.Context, c *ent.Client, releaseID uuid.UUID, action Action,
	from, to *int, fromVer, toVer *uuid.UUID, detail string) error {
	if err := c.ReleaseEvent.Create().
		SetReleaseID(releaseID).
		SetAction(string(action)).
		SetNillableFromWeight(from).
		SetNillableToWeight(to).
		SetNillableFromVersion(fromVer).
		SetNillableToVersion(toVer).
		SetDetail(detail).
		SetCreatedAt(time.Now()).
		Exec(ctx); err != nil {
		return fmt.Errorf("insert release event: %w", err)
	}
	return nil
}

func intPtr(i int) *int { return &i }
