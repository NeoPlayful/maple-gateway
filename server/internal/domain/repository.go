package domain

import (
	"context"
	"errors"
	"fmt"

	"github.com/NeoPlayful/maple-gateway/server/pkg"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Repository 是 Domain 数据访问层。
type Repository struct {
	pool *pgxpool.Pool
}

// NewRepository 构造。
func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

const cols = `id, tenant_id, hostname, service_id, status, verified_at, created_at, updated_at`

func scanDomain(row pgx.Row) (*Domain, error) {
	var d Domain
	err := row.Scan(&d.ID, &d.TenantID, &d.Hostname, &d.ServiceID, &d.Status, &d.VerifiedAt, &d.CreatedAt, &d.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &d, nil
}

// Create 插入新域名。hostname 冲突返回 Conflict。
func (r *Repository) Create(ctx context.Context, in New) (*Domain, error) {
	row := r.pool.QueryRow(ctx, `
		INSERT INTO domains(tenant_id, hostname, service_id, status)
		VALUES($1, $2, $3, 'active')
		RETURNING `+cols,
		in.TenantID, in.Hostname, in.ServiceID)
	d, err := scanDomain(row)
	if err != nil {
		if pkg.IsUniqueViolation(err) {
			return nil, pkg.ErrConflict("hostname 已存在")
		}
		return nil, fmt.Errorf("insert domain: %w", err)
	}
	return d, nil
}

// GetByID 查询。
func (r *Repository) GetByID(ctx context.Context, id uuid.UUID) (*Domain, error) {
	d, err := scanDomain(r.pool.QueryRow(ctx, `SELECT `+cols+` FROM domains WHERE id=$1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, pkg.ErrNotFound("域名不存在")
	}
	if err != nil {
		return nil, fmt.Errorf("get domain: %w", err)
	}
	return d, nil
}

// GetByHostname 查询（数据平面路由用）。
func (r *Repository) GetByHostname(ctx context.Context, hostname string) (*Domain, error) {
	d, err := scanDomain(r.pool.QueryRow(ctx, `SELECT `+cols+` FROM domains WHERE hostname=$1`, hostname))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, pkg.ErrNotFound("域名不存在")
	}
	if err != nil {
		return nil, fmt.Errorf("get domain by hostname: %w", err)
	}
	return d, nil
}

// ListByTenant 列出某租户的域名。
func (r *Repository) ListByTenant(ctx context.Context, tenantID uuid.UUID) ([]*Domain, error) {
	rows, err := r.pool.Query(ctx, `SELECT `+cols+` FROM domains WHERE tenant_id=$1 ORDER BY hostname`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("list domains: %w", err)
	}
	defer rows.Close()
	out := []*Domain{}
	for rows.Next() {
		d, err := scanDomain(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// All 返回全部域名（路由缓存构建用）。
func (r *Repository) All(ctx context.Context) ([]*Domain, error) {
	rows, err := r.pool.Query(ctx, `SELECT `+cols+` FROM domains ORDER BY hostname`)
	if err != nil {
		return nil, fmt.Errorf("all domains: %w", err)
	}
	defer rows.Close()
	out := []*Domain{}
	for rows.Next() {
		d, err := scanDomain(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// List 全量域名（分页）。
func (r *Repository) List(ctx context.Context, limit, offset int) ([]*Domain, int, error) {
	var total int
	if err := r.pool.QueryRow(ctx, `SELECT count(*) FROM domains`).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := r.pool.Query(ctx, `SELECT `+cols+` FROM domains ORDER BY hostname LIMIT $1 OFFSET $2`, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []*Domain{}
	for rows.Next() {
		d, err := scanDomain(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, d)
	}
	return out, total, rows.Err()
}

// Update 应用非空更新。
func (r *Repository) Update(ctx context.Context, id uuid.UUID, in Update) (*Domain, error) {
	d, err := r.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if in.Hostname != nil {
		d.Hostname = *in.Hostname
	}
	if in.ServiceID != nil {
		d.ServiceID = in.ServiceID
	}
	if in.Status != nil {
		d.Status = *in.Status
	}
	err = r.pool.QueryRow(ctx, `
		UPDATE domains SET hostname=$2, service_id=$3, status=$4, updated_at=now()
		WHERE id=$1 RETURNING `+cols,
		id, d.Hostname, d.ServiceID, d.Status).Scan(&d.ID, &d.TenantID, &d.Hostname, &d.ServiceID, &d.Status, &d.VerifiedAt, &d.CreatedAt, &d.UpdatedAt)
	if err != nil {
		if pkg.IsUniqueViolation(err) {
			return nil, pkg.ErrConflict("hostname 已存在")
		}
		return nil, fmt.Errorf("update domain: %w", err)
	}
	return d, nil
}

// Delete 删除。
func (r *Repository) Delete(ctx context.Context, id uuid.UUID) error {
	tag, err := r.pool.Exec(ctx, `DELETE FROM domains WHERE id=$1`, id)
	if err != nil {
		return fmt.Errorf("delete domain: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return pkg.ErrNotFound("域名不存在")
	}
	return nil
}
