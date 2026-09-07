package bluegreen

import (
	"context"
	"errors"
	"fmt"

	"github.com/NeoPlayful/maple-gateway/server/pkg"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Repository 是 Blue/Green 数据访问层。
type Repository struct {
	pool *pgxpool.Pool
}

// NewRepository 构造。
func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

const cols = `id, deployment_id, blue_version_id, green_version_id, active_version_id,
	previous_active_id, created_at, updated_at`

func scanBG(row pgx.Row) (*BGDeployment, error) {
	var b BGDeployment
	err := row.Scan(&b.ID, &b.DeploymentID, &b.BlueVersionID, &b.GreenVersionID,
		&b.ActiveVersionID, &b.PreviousActiveID, &b.CreatedAt, &b.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &b, nil
}

// Create 登记 BG 配置（deployment 级唯一）。
func (r *Repository) Create(ctx context.Context, in NewBG) (*BGDeployment, error) {
	if in.BlueVersionID == in.GreenVersionID {
		return nil, pkg.ErrValidation("blue 与 green 版本不能相同")
	}
	active := in.InitialActive
	if active == uuid.Nil {
		active = in.BlueVersionID
	}
	row := r.pool.QueryRow(ctx, `
		INSERT INTO bluegreen_deployments(deployment_id, blue_version_id, green_version_id,
			active_version_id, previous_active_id)
		VALUES($1, $2, $3, $4, NULL)
		RETURNING `+cols,
		in.DeploymentID, in.BlueVersionID, in.GreenVersionID, active)
	bg, err := scanBG(row)
	if err != nil {
		if pkg.IsUniqueViolation(err) {
			return nil, pkg.ErrConflict("该部署已配置 Blue/Green")
		}
		if pkg.IsForeignKeyViolation(err) {
			return nil, pkg.ErrValidation("deployment_id 或版本 ID 不存在")
		}
		return nil, fmt.Errorf("insert bluegreen: %w", err)
	}
	return bg, nil
}

// Get 查询。
func (r *Repository) Get(ctx context.Context, id uuid.UUID) (*BGDeployment, error) {
	bg, err := scanBG(r.pool.QueryRow(ctx, `SELECT `+cols+` FROM bluegreen_deployments WHERE id=$1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, pkg.ErrNotFound("Blue/Green 配置不存在")
	}
	if err != nil {
		return nil, fmt.Errorf("get bluegreen: %w", err)
	}
	return bg, nil
}

// List 分页列出（deploymentID 可选）。
func (r *Repository) List(ctx context.Context, deploymentID *uuid.UUID, limit, offset int) ([]*BGDeployment, int, error) {
	var total int
	if err := r.pool.QueryRow(ctx, `
		SELECT count(*) FROM bluegreen_deployments WHERE ($1::uuid IS NULL OR deployment_id=$1)`,
		deploymentID).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count bluegreen: %w", err)
	}
	rows, err := r.pool.Query(ctx, `
		SELECT `+cols+` FROM bluegreen_deployments
		WHERE ($1::uuid IS NULL OR deployment_id=$1)
		ORDER BY created_at DESC LIMIT $2 OFFSET $3`, deploymentID, limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("list bluegreen: %w", err)
	}
	defer rows.Close()
	out := []*BGDeployment{}
	for rows.Next() {
		bg, err := scanBG(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, bg)
	}
	return out, total, rows.Err()
}

// Delete 删除 BG 配置（不改变版本状态）。
func (r *Repository) Delete(ctx context.Context, id uuid.UUID) error {
	tag, err := r.pool.Exec(ctx, `DELETE FROM bluegreen_deployments WHERE id=$1`, id)
	if err != nil {
		return fmt.Errorf("delete bluegreen: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return pkg.ErrNotFound("Blue/Green 配置不存在")
	}
	return nil
}

// Transition 事务内执行角色翻转动作。
func (r *Repository) Transition(ctx context.Context, id uuid.UUID,
	fn func(ctx context.Context, tx pgx.Tx, bg *BGDeployment) error) (*BGDeployment, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	bg, err := scanBG(tx.QueryRow(ctx,
		`SELECT `+cols+` FROM bluegreen_deployments WHERE id=$1 FOR UPDATE`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, pkg.ErrNotFound("Blue/Green 配置不存在")
	}
	if err != nil {
		return nil, err
	}
	if err := fn(ctx, tx, bg); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return r.Get(ctx, id)
}

// ---------- 事务内 helper ----------

// flipActive 把 bg.active 翻转为 target：active 版本 weight 100 + 状态 active；
// 另一版本 weight 0 + standby。recordPrev 为 true 时把翻转前的 active 记为 previous（供回滚）。
func flipActive(ctx context.Context, tx pgx.Tx, bg *BGDeployment, target uuid.UUID, recordPrev bool) error {
	if target != bg.BlueVersionID && target != bg.GreenVersionID {
		return pkg.ErrValidation("target 必须是 blue 或 green 版本")
	}
	other := bg.BlueVersionID
	if target == bg.BlueVersionID {
		other = bg.GreenVersionID
	}
	if err := setVersionRole(ctx, tx, target, "active", 100); err != nil {
		return err
	}
	if err := setVersionRole(ctx, tx, other, "standby", 0); err != nil {
		return err
	}
	if recordPrev {
		if _, err := tx.Exec(ctx, `
			UPDATE bluegreen_deployments SET active_version_id=$2::uuid,
				previous_active_id=$3::uuid, updated_at=now()
			WHERE id=$1::uuid`, bg.ID, target, bg.ActiveVersionID); err != nil {
			return fmt.Errorf("update bluegreen active: %w", err)
		}
	} else {
		// initial 激活：previous 无意义，置 NULL。
		if _, err := tx.Exec(ctx, `
			UPDATE bluegreen_deployments SET active_version_id=$2::uuid,
				previous_active_id=NULL, updated_at=now()
			WHERE id=$1::uuid`, bg.ID, target); err != nil {
			return fmt.Errorf("update bluegreen initial active: %w", err)
		}
	}
	return nil
}

func setVersionRole(ctx context.Context, tx pgx.Tx, id uuid.UUID, status string, weight int) error {
	if _, err := tx.Exec(ctx, `
		UPDATE deployment_versions SET status=$2, weight=$3, updated_at=now() WHERE id=$1::uuid`,
		id, status, weight); err != nil {
		return fmt.Errorf("set version role: %w", err)
	}
	return nil
}

// insertEvent 写切换历史。
func insertEvent(ctx context.Context, tx pgx.Tx, bgID uuid.UUID, action string,
	from, to uuid.UUID, detail string) error {
	if _, err := tx.Exec(ctx, `
		INSERT INTO bluegreen_events(bg_id, action, from_active, to_active, detail)
		VALUES($1, $2, $3, $4, $5)`, bgID, action, from, to, detail); err != nil {
		return fmt.Errorf("insert bluegreen event: %w", err)
	}
	return nil
}
