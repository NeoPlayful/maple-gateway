package canary

import (
	"context"
	"errors"
	"fmt"

	"github.com/NeoPlayful/maple-gateway/server/pkg"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Repository 是 Canary 发布数据访问层。
type Repository struct {
	pool *pgxpool.Pool
}

// NewRepository 构造。
func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
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
	row := r.pool.QueryRow(ctx, `
		INSERT INTO canary_releases(service_id, name, stable_version_id, canary_version_id,
			phase, canary_weight, target_weight, step_weight)
		VALUES($1, $2, $3, $4, 'created', 0, $5, $6)
		RETURNING `+cols,
		in.ServiceID, in.Name, in.StableVersionID, in.CanaryVersionID, target, step)
	rel, err := scanRelease(row)
	if err != nil {
		if pkg.IsUniqueViolation(err) {
			return nil, pkg.ErrConflict("该服务下已存在同名发布")
		}
		if pkg.IsForeignKeyViolation(err) {
			return nil, pkg.ErrValidation("service_id 或版本 ID 不存在")
		}
		return nil, fmt.Errorf("insert canary release: %w", err)
	}
	return rel, nil
}

// Get 查询发布。
func (r *Repository) Get(ctx context.Context, id uuid.UUID) (*Release, error) {
	rel, err := scanRelease(r.pool.QueryRow(ctx, `SELECT `+cols+` FROM canary_releases WHERE id=$1`, id))
	if e := noRows(err, "发布不存在"); e != nil {
		return nil, e
	}
	if err != nil {
		return nil, fmt.Errorf("get canary release: %w", err)
	}
	return rel, nil
}

// List 分页列出发布，支持 service_id 筛选与 phase 筛选。
func (r *Repository) List(ctx context.Context, serviceID *uuid.UUID, phase Phase, limit, offset int) ([]*Release, int, error) {
	var total int
	if err := r.pool.QueryRow(ctx, `
		SELECT count(*) FROM canary_releases
		WHERE ($1::uuid IS NULL OR service_id=$1) AND ($2='' OR phase=$2)`,
		serviceID, string(phase)).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count canary releases: %w", err)
	}
	rows, err := r.pool.Query(ctx, `
		SELECT `+cols+` FROM canary_releases
		WHERE ($1::uuid IS NULL OR service_id=$1) AND ($2='' OR phase=$2)
		ORDER BY created_at DESC LIMIT $3 OFFSET $4`, serviceID, string(phase), limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("list canary releases: %w", err)
	}
	defer rows.Close()
	out := []*Release{}
	for rows.Next() {
		rel, err := scanRelease(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, rel)
	}
	return out, total, rows.Err()
}

// Update 应用非空更新（name/target_weight/step_weight），不改 phase 与实时权重。
func (r *Repository) Update(ctx context.Context, id uuid.UUID, in UpdateRelease) (*Release, error) {
	rel, err := r.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if in.Name != nil {
		rel.Name = *in.Name
	}
	if in.TargetWeight != nil {
		rel.TargetWeight = *in.TargetWeight
	}
	if in.StepWeight != nil {
		rel.StepWeight = *in.StepWeight
	}
	upd, err := scanRelease(r.pool.QueryRow(ctx, `
		UPDATE canary_releases SET name=$2, target_weight=$3, step_weight=$4, updated_at=now()
		WHERE id=$1 RETURNING `+cols,
		id, rel.Name, rel.TargetWeight, rel.StepWeight))
	if err != nil {
		if pkg.IsUniqueViolation(err) {
			return nil, pkg.ErrConflict("该服务下已存在同名发布")
		}
		return nil, fmt.Errorf("update canary release: %w", err)
	}
	return upd, nil
}

// Delete 删除发布（不改变版本权重；调用方需确认已结束）。
func (r *Repository) Delete(ctx context.Context, id uuid.UUID) error {
	tag, err := r.pool.Exec(ctx, `DELETE FROM canary_releases WHERE id=$1`, id)
	if err != nil {
		return fmt.Errorf("delete canary release: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return pkg.ErrNotFound("发布不存在")
	}
	return nil
}

// ---------- 事务内状态机原子操作 ----------

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
	sql := `UPDATE canary_releases SET phase=$2, canary_weight=$3, updated_at=now()`
	args := []any{id, string(phase), canaryWeight}
	if start {
		sql += `, started_at=COALESCE(started_at, now())`
	}
	if finish {
		sql += `, finished_at=now()`
	}
	sql += ` WHERE id=$1::uuid RETURNING ` + cols
	rel, err := scanRelease(tx.QueryRow(ctx, sql, args...))
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
