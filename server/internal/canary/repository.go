package canary

import (
	"context"
	"errors"
	"fmt"
	"time"

	"entgo.io/ent/dialect/sql"
	"github.com/NeoPlayful/maple-gateway/server/ent"
	entcanary "github.com/NeoPlayful/maple-gateway/server/ent/canaryrelease"
	"github.com/NeoPlayful/maple-gateway/server/pkg"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Repository 是 Canary 发布数据访问层。
// 普通 CRUD 走 Ent；Transition 事务链（FOR UPDATE 行锁 + 跨表版本写）保留 pgxpool。
type Repository struct {
	ent  *ent.Client
	pool *pgxpool.Pool
}

// NewRepository 构造。
func NewRepository(client *ent.Client, pool *pgxpool.Pool) *Repository {
	return &Repository{ent: client, pool: pool}
}

const cols = `id, service_id, name, stable_version_id, canary_version_id, phase,
	canary_weight, target_weight, step_weight, started_at, finished_at, created_at, updated_at`

func scanRelease(row pgx.Row) (*Release, error) {
	var r Release
	err := row.Scan(&r.ID, &r.ServiceID, &r.Name, &r.StableVersionID, &r.CanaryVersionID,
		&r.Phase, &r.CanaryWeight, &r.TargetWeight, &r.StepWeight,
		&r.StartedAt, &r.FinishedAt, &r.CreatedAt, &r.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &r, nil
}

func noRows(err error, msg string) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return pkg.ErrNotFound(msg)
	}
	return err
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

// ---------- 事务内状态机原子操作（保留 pgxpool，service 层零改动） ----------

// Transition 开启事务执行状态机动作：行锁发布 → fn(tx, rel) → 提交。
// fn 内通过包级 helper（setVersionWeight 等）操作版本；返回错误则整单回滚。
func (r *Repository) Transition(ctx context.Context, id uuid.UUID,
	fn func(ctx context.Context, tx pgx.Tx, rel *Release) error) (*Release, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	rel, err := lockRelease(ctx, tx, id)
	if err != nil {
		return nil, err
	}
	if err := fn(ctx, tx, rel); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return r.Get(ctx, id)
}

// lockRelease 行锁发布，返回当前值（防并发 start/promote）。
func lockRelease(ctx context.Context, tx pgx.Tx, id uuid.UUID) (*Release, error) {
	rel, err := scanRelease(tx.QueryRow(ctx,
		`SELECT `+cols+` FROM canary_releases WHERE id=$1 FOR UPDATE`, id))
	if e := noRows(err, "发布不存在"); e != nil {
		return nil, e
	}
	if err != nil {
		return nil, fmt.Errorf("lock canary release: %w", err)
	}
	return rel, nil
}

// versionRef 事务内读取版本（weight/status/version 名），用于校验与快照。
func versionRef(ctx context.Context, tx pgx.Tx, id uuid.UUID) (weight int, status string, version string, deploymentID uuid.UUID, err error) {
	err = tx.QueryRow(ctx, `
		SELECT weight, status, version, deployment_id FROM deployment_versions WHERE id=$1`, id).
		Scan(&weight, &status, &version, &deploymentID)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, "", "", uuid.Nil, pkg.ErrNotFound("版本不存在")
	}
	return weight, status, version, deploymentID, err
}

// setVersionWeight 事务内改版本权重。
func setVersionWeight(ctx context.Context, tx pgx.Tx, id uuid.UUID, weight int) error {
	if _, err := tx.Exec(ctx,
		`UPDATE deployment_versions SET weight=$2, updated_at=now() WHERE id=$1`, id, weight); err != nil {
		return fmt.Errorf("set version weight: %w", err)
	}
	return nil
}

// setVersionStatus 事务内改版本状态（stable/canary/standby/draining/inactive）。
func setVersionStatus(ctx context.Context, tx pgx.Tx, id uuid.UUID, status string) error {
	if _, err := tx.Exec(ctx,
		`UPDATE deployment_versions SET status=$2, updated_at=now() WHERE id=$1`, id, status); err != nil {
		return fmt.Errorf("set version status: %w", err)
	}
	return nil
}

// updateReleaseState 事务内改发布 phase/权重/时间，并返回更新后的值。
func updateReleaseState(ctx context.Context, tx pgx.Tx, id uuid.UUID, phase Phase,
	canaryWeight int, start, finish bool) (*Release, error) {
	// $1 显式 ::uuid：动态拼接时 PostgreSQL 无法从 UPDATE...RETURNING 推断参数类型（42P18）。
	q := `UPDATE canary_releases SET phase=$2, canary_weight=$3, updated_at=now()`
	args := []any{id, string(phase), canaryWeight}
	if start {
		q += `, started_at=COALESCE(started_at, now())`
	}
	if finish {
		q += `, finished_at=now()`
	}
	q += ` WHERE id=$1::uuid RETURNING ` + cols
	rel, err := scanRelease(tx.QueryRow(ctx, q, args...))
	if err != nil {
		return nil, fmt.Errorf("update canary release state: %w", err)
	}
	return rel, nil
}

// insertEvent 事务内写事件流水。
func insertEvent(ctx context.Context, tx pgx.Tx, releaseID uuid.UUID, action Action,
	from, to *int, detail string) error {
	if _, err := tx.Exec(ctx, `
		INSERT INTO canary_events(release_id, phase, from_weight, to_weight, detail)
		VALUES($1, $2, $3, $4, $5)`,
		releaseID, string(action), from, to, detail); err != nil {
		return fmt.Errorf("insert canary event: %w", err)
	}
	return nil
}
