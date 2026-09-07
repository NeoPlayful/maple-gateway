package deployment

import (
	"context"
	"errors"
	"fmt"

	"github.com/NeoPlayful/maple-gateway/server/pkg"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Repository 是 Deployment / Version 数据访问层。
type Repository struct {
	pool *pgxpool.Pool
}

// NewRepository 构造。
func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

const dCols = `id, service_id, name, status, strategy, created_at, updated_at`
const vCols = `id, deployment_id, version, image, weight, status, created_at, updated_at`

func scanDeployment(row pgx.Row) (*Deployment, error) {
	var d Deployment
	err := row.Scan(&d.ID, &d.ServiceID, &d.Name, &d.Status, &d.Strategy, &d.CreatedAt, &d.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &d, nil
}

func scanVersion(row pgx.Row) (*Version, error) {
	var v Version
	err := row.Scan(&v.ID, &v.DeploymentID, &v.Version, &v.Image, &v.Weight, &v.Status,
		&v.CreatedAt, &v.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &v, nil
}

func errNoRows(err error, kind string) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return pkg.ErrNotFound(kind)
	}
	return nil
}

// ---------- Deployment ----------

// CreateDeployment 插入部署。service_id 不存在走外键错误，同名冲突走唯一冲突。
func (r *Repository) CreateDeployment(ctx context.Context, in NewDeployment) (*Deployment, error) {
	strategy := in.Strategy
	if strategy == "" {
		strategy = StrategyRolling
	}
	row := r.pool.QueryRow(ctx, `
		INSERT INTO deployments(service_id, name, status, strategy)
		VALUES($1, $2, 'active', $3)
		RETURNING `+dCols,
		in.ServiceID, in.Name, strategy)
	d, err := scanDeployment(row)
	if err != nil {
		if pkg.IsUniqueViolation(err) {
			return nil, pkg.ErrConflict("该服务下已存在同名部署")
		}
		if pkg.IsForeignKeyViolation(err) {
			return nil, pkg.ErrValidation("服务不存在")
		}
		return nil, fmt.Errorf("insert deployment: %w", err)
	}
	return d, nil
}

// GetDeployment 查询部署。
func (r *Repository) GetDeployment(ctx context.Context, id uuid.UUID) (*Deployment, error) {
	d, err := scanDeployment(r.pool.QueryRow(ctx, `SELECT `+dCols+` FROM deployments WHERE id=$1`, id))
	if e := errNoRows(err, "部署不存在"); e != nil {
		return nil, e
	}
	if err != nil {
		return nil, fmt.Errorf("get deployment: %w", err)
	}
	return d, nil
}

// ListDeployments 分页列出部署，支持 service_id 筛选（nil 表示全部）。
func (r *Repository) ListDeployments(ctx context.Context, serviceID *uuid.UUID, limit, offset int) ([]*Deployment, int, error) {
	var total int
	if err := r.pool.QueryRow(ctx,
		`SELECT count(*) FROM deployments WHERE ($1::uuid IS NULL OR service_id=$1)`,
		serviceID).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count deployments: %w", err)
	}
	rows, err := r.pool.Query(ctx, `
		SELECT `+dCols+` FROM deployments
		WHERE ($1::uuid IS NULL OR service_id=$1)
		ORDER BY created_at DESC
		LIMIT $2 OFFSET $3`, serviceID, limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("list deployments: %w", err)
	}
	defer rows.Close()
	out := []*Deployment{}
	for rows.Next() {
		d, err := scanDeployment(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, d)
	}
	return out, total, rows.Err()
}

// UpdateDeployment 应用非空更新。
func (r *Repository) UpdateDeployment(ctx context.Context, id uuid.UUID, in UpdateDeployment) (*Deployment, error) {
	d, err := r.GetDeployment(ctx, id)
	if err != nil {
		return nil, err
	}
	if in.Name != nil {
		d.Name = *in.Name
	}
	if in.Status != nil {
		d.Status = *in.Status
	}
	if in.Strategy != nil {
		d.Strategy = *in.Strategy
	}
	err = r.pool.QueryRow(ctx, `
		UPDATE deployments SET name=$2, status=$3, strategy=$4, updated_at=now()
		WHERE id=$1 RETURNING `+dCols,
		id, d.Name, d.Status, d.Strategy).Scan(&d.ID, &d.ServiceID, &d.Name, &d.Status, &d.Strategy,
		&d.CreatedAt, &d.UpdatedAt)
	if err != nil {
		if pkg.IsUniqueViolation(err) {
			return nil, pkg.ErrConflict("该服务下已存在同名部署")
		}
		return nil, fmt.Errorf("update deployment: %w", err)
	}
	return d, nil
}

// SetDeploymentStatus 便捷状态变更。
func (r *Repository) SetDeploymentStatus(ctx context.Context, id uuid.UUID, s Status) (*Deployment, error) {
	return r.UpdateDeployment(ctx, id, UpdateDeployment{Status: &s})
}

// DeleteDeployment 删除部署（级联删除版本；其上实例的 deployment_id 置空）。
func (r *Repository) DeleteDeployment(ctx context.Context, id uuid.UUID) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	if _, err := tx.Exec(ctx,
		`UPDATE instances SET deployment_id=NULL, updated_at=now() WHERE deployment_id=$1`, id); err != nil {
		return fmt.Errorf("detach instances from deployment: %w", err)
	}
	tag, err := tx.Exec(ctx, `DELETE FROM deployments WHERE id=$1`, id)
	if err != nil {
		return fmt.Errorf("delete deployment: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return pkg.ErrNotFound("部署不存在")
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	return nil
}

// ---------- Version ----------

// CreateVersion 在部署下新增版本。同 deployment 内版本号唯一。
func (r *Repository) CreateVersion(ctx context.Context, deploymentID uuid.UUID, in NewVersion) (*Version, error) {
	weight := in.Weight
	if weight == 0 {
		weight = 100
	}
	status := in.Status
	if status == "" {
		status = VersionStable
	}
	row := r.pool.QueryRow(ctx, `
		INSERT INTO deployment_versions(deployment_id, version, image, weight, status)
		VALUES($1, $2, $3, $4, $5)
		RETURNING `+vCols,
		deploymentID, in.Version, in.Image, weight, status)
	v, err := scanVersion(row)
	if err != nil {
		if pkg.IsUniqueViolation(err) {
			return nil, pkg.ErrConflict("该部署下已存在同名版本")
		}
		if pkg.IsForeignKeyViolation(err) {
			return nil, pkg.ErrValidation("部署不存在")
		}
		return nil, fmt.Errorf("insert version: %w", err)
	}
	return v, nil
}

// GetVersion 查询版本。
func (r *Repository) GetVersion(ctx context.Context, id uuid.UUID) (*Version, error) {
	v, err := scanVersion(r.pool.QueryRow(ctx, `SELECT `+vCols+` FROM deployment_versions WHERE id=$1`, id))
	if e := errNoRows(err, "版本不存在"); e != nil {
		return nil, e
	}
	if err != nil {
		return nil, fmt.Errorf("get version: %w", err)
	}
	return v, nil
}

// ListVersions 列出某部署的全部版本。
func (r *Repository) ListVersions(ctx context.Context, deploymentID uuid.UUID) ([]*Version, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT `+vCols+` FROM deployment_versions WHERE deployment_id=$1
		ORDER BY created_at`, deploymentID)
	if err != nil {
		return nil, fmt.Errorf("list versions: %w", err)
	}
	defer rows.Close()
	out := []*Version{}
	for rows.Next() {
		v, err := scanVersion(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// ListVersionsByDeployments 批量列出多个部署的版本（路由表构建用）。
func (r *Repository) ListVersionsByDeployments(ctx context.Context, deploymentIDs []uuid.UUID) ([]*Version, error) {
	if len(deploymentIDs) == 0 {
		return []*Version{}, nil
	}
	rows, err := r.pool.Query(ctx, `
		SELECT `+vCols+` FROM deployment_versions WHERE deployment_id = ANY($1)
		ORDER BY deployment_id, created_at`, deploymentIDs)
	if err != nil {
		return nil, fmt.Errorf("list versions by deployments: %w", err)
	}
	defer rows.Close()
	out := []*Version{}
	for rows.Next() {
		v, err := scanVersion(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// UpdateVersion 应用非空更新（image/weight/status）。
func (r *Repository) UpdateVersion(ctx context.Context, id uuid.UUID, in UpdateVersion) (*Version, error) {
	v, err := r.GetVersion(ctx, id)
	if err != nil {
		return nil, err
	}
	if in.Image != nil {
		v.Image = *in.Image
	}
	if in.Weight != nil {
		v.Weight = *in.Weight
	}
	if in.Status != nil {
		v.Status = *in.Status
	}
	err = r.pool.QueryRow(ctx, `
		UPDATE deployment_versions SET image=$2, weight=$3, status=$4, updated_at=now()
		WHERE id=$1 RETURNING `+vCols,
		id, v.Image, v.Weight, v.Status).Scan(&v.ID, &v.DeploymentID, &v.Version, &v.Image, &v.Weight,
		&v.Status, &v.CreatedAt, &v.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("update version: %w", err)
	}
	return v, nil
}

// SetVersionStatus 便捷状态变更。
func (r *Repository) SetVersionStatus(ctx context.Context, id uuid.UUID, s VersionStatus) (*Version, error) {
	return r.UpdateVersion(ctx, id, UpdateVersion{Status: &s})
}

// DeleteVersion 删除版本，并清空引用它的实例的 version_id（避免路由漂移）。
func (r *Repository) DeleteVersion(ctx context.Context, id uuid.UUID) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	if _, err := tx.Exec(ctx,
		`UPDATE instances SET version_id=NULL, updated_at=now() WHERE version_id=$1`, id); err != nil {
		return fmt.Errorf("detach instances from version: %w", err)
	}
	tag, err := tx.Exec(ctx, `DELETE FROM deployment_versions WHERE id=$1`, id)
	if err != nil {
		return fmt.Errorf("delete version: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return pkg.ErrNotFound("版本不存在")
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	return nil
}

// VersionGroupRow 是跨 service 的版本聚合行（路由表构建用，避免引入 cache 依赖）。
type VersionGroupRow struct {
	ServiceID    uuid.UUID
	DeploymentID uuid.UUID
	VersionID    uuid.UUID
	Version      string
	Weight       int
	Status       VersionStatus
}

// AllVersionGroups 返回全部 deployments × deployment_versions 的聚合行，供路由表构建。
func (r *Repository) AllVersionGroups(ctx context.Context) ([]VersionGroupRow, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT d.service_id, dv.deployment_id, dv.id, dv.version, dv.weight, dv.status
		FROM deployment_versions dv
		JOIN deployments d ON d.id = dv.deployment_id
		ORDER BY d.service_id, dv.created_at`)
	if err != nil {
		return nil, fmt.Errorf("all version groups: %w", err)
	}
	defer rows.Close()
	out := []VersionGroupRow{}
	for rows.Next() {
		var g VersionGroupRow
		if err := rows.Scan(&g.ServiceID, &g.DeploymentID, &g.VersionID, &g.Version,
			&g.Weight, &g.Status); err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

// SetDefaultVersion 将部署内某版本设为 stable，其余 stable 降为 standby。
// 返回被更新的其余版本（供 S2 路由表同步）。若某版本有 healthy 实例，调用方后续处理。
func (r *Repository) SetDefaultVersion(ctx context.Context, deploymentID, versionID uuid.UUID) (*Version, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	// 校验目标版本属于该部署。
	var cur Version
	err = tx.QueryRow(ctx, `SELECT `+vCols+` FROM deployment_versions WHERE id=$1 FOR UPDATE`, versionID).
		Scan(&cur.ID, &cur.DeploymentID, &cur.Version, &cur.Image, &cur.Weight, &cur.Status,
			&cur.CreatedAt, &cur.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, pkg.ErrNotFound("版本不存在")
	}
	if err != nil {
		return nil, err
	}
	if cur.DeploymentID != deploymentID {
		return nil, pkg.ErrValidation("版本不属于该部署")
	}

	// 同部署内其余可路由版本（stable/canary/active/standby/draining）→ standby，
	// 保证任何时刻仅一个 stable（canary 目标版本由 S4 另行管理）。
	if _, err := tx.Exec(ctx, `
		UPDATE deployment_versions SET status='standby', updated_at=now()
		WHERE deployment_id=$1 AND status IN ('stable','canary','active','standby','draining') AND id<>$2`,
		deploymentID, versionID); err != nil {
		return nil, fmt.Errorf("demote old versions: %w", err)
	}
	row := tx.QueryRow(ctx, `
		UPDATE deployment_versions SET status='stable', updated_at=now()
		WHERE id=$1 RETURNING `+vCols, versionID)
	v, err := scanVersion(row)
	if err != nil {
		return nil, fmt.Errorf("promote version: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return v, nil
}
