package tenant

import (
	"context"
	"errors"
	"fmt"

	"github.com/NeoPlayful/maple-gateway/server/pkg"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Repository 是 Tenant 数据访问层。
type Repository struct {
	pool *pgxpool.Pool
}

// NewRepository 构造。
func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

const cols = `id, name, slug, status, description, created_at, updated_at`

func scanTenant(row pgx.Row) (*Tenant, error) {
	var t Tenant
	err := row.Scan(&t.ID, &t.Name, &t.Slug, &t.Status, &t.Description, &t.CreatedAt, &t.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

// Create 插入新租户。slug 冲突返回 Conflict。
func (r *Repository) Create(ctx context.Context, in New) (*Tenant, error) {
	row := r.pool.QueryRow(ctx, `
		INSERT INTO tenants(name, slug, status, description)
		VALUES($1, $2, 'active', $3)
		RETURNING `+cols,
		in.Name, in.Slug, in.Description)
	t, err := scanTenant(row)
	if err != nil {
		if pkg.IsUniqueViolation(err) {
			return nil, pkg.ErrConflict("slug 已存在")
		}
		return nil, fmt.Errorf("insert tenant: %w", err)
	}
	return t, nil
}

// GetByID 查询单个租户。
func (r *Repository) GetByID(ctx context.Context, id uuid.UUID) (*Tenant, error) {
	row := r.pool.QueryRow(ctx, `SELECT `+cols+` FROM tenants WHERE id=$1`, id)
	t, err := scanTenant(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, pkg.ErrNotFound("租户不存在")
	}
	if err != nil {
		return nil, fmt.Errorf("get tenant: %w", err)
	}
	return t, nil
}

// All 返回全部租户（路由缓存构建用）。
func (r *Repository) All(ctx context.Context) ([]*Tenant, error) {
	rows, err := r.pool.Query(ctx, `SELECT `+cols+` FROM tenants`)
	if err != nil {
		return nil, fmt.Errorf("all tenants: %w", err)
	}
	defer rows.Close()
	out := []*Tenant{}
	for rows.Next() {
		t, err := scanTenant(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// List 分页列出租户，支持按状态筛选。
func (r *Repository) List(ctx context.Context, status Status, limit, offset int) ([]*Tenant, int, error) {
	var total int
	if err := r.pool.QueryRow(ctx, `SELECT count(*) FROM tenants WHERE ($1='' OR status=$1)`,
		string(status)).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count tenants: %w", err)
	}
	rows, err := r.pool.Query(ctx, `
		SELECT `+cols+` FROM tenants
		WHERE ($1='' OR status=$1)
		ORDER BY created_at DESC
		LIMIT $2 OFFSET $3`, string(status), limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("list tenants: %w", err)
	}
	defer rows.Close()

	out := []*Tenant{}
	for rows.Next() {
		t, err := scanTenant(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, t)
	}
	return out, total, rows.Err()
}

// Update 应用非空更新。
func (r *Repository) Update(ctx context.Context, id uuid.UUID, in Update) (*Tenant, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck // commit 成功后再 rollback 无害

	var t Tenant
	err = tx.QueryRow(ctx, `SELECT `+cols+` FROM tenants WHERE id=$1 FOR UPDATE`, id).
		Scan(&t.ID, &t.Name, &t.Slug, &t.Status, &t.Description, &t.CreatedAt, &t.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, pkg.ErrNotFound("租户不存在")
	}
	if err != nil {
		return nil, err
	}
	if in.Name != nil {
		t.Name = *in.Name
	}
	if in.Description != nil {
		t.Description = *in.Description
	}
	if in.Status != nil {
		t.Status = *in.Status
	}

	err = tx.QueryRow(ctx, `
		UPDATE tenants SET name=$2, description=$3, status=$4, updated_at=now()
		WHERE id=$1 RETURNING `+cols,
		id, t.Name, t.Description, t.Status).Scan(&t.ID, &t.Name, &t.Slug, &t.Status, &t.Description, &t.CreatedAt, &t.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("update tenant: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &t, nil
}

// SetStatus 便捷状态变更。
func (r *Repository) SetStatus(ctx context.Context, id uuid.UUID, s Status) (*Tenant, error) {
	return r.Update(ctx, id, Update{Status: &s})
}

// Delete 删除租户。
func (r *Repository) Delete(ctx context.Context, id uuid.UUID) error {
	tag, err := r.pool.Exec(ctx, `DELETE FROM tenants WHERE id=$1`, id)
	if err != nil {
		return fmt.Errorf("delete tenant: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return pkg.ErrNotFound("租户不存在")
	}
	return nil
}
