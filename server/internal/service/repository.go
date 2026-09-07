package service

import (
	"context"
	"errors"
	"fmt"

	"github.com/NeoPlayful/maple-gateway/server/pkg"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Repository 是 Service 数据访问层。
type Repository struct {
	pool *pgxpool.Pool
}

// NewRepository 构造。
func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

const cols = `id, tenant_id, name, protocol, status, created_at, updated_at`

func scanService(row pgx.Row) (*Service, error) {
	var s Service
	err := row.Scan(&s.ID, &s.TenantID, &s.Name, &s.Protocol, &s.Status, &s.CreatedAt, &s.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &s, nil
}

// Create 插入。tenant 内 name 冲突返回 Conflict。
func (r *Repository) Create(ctx context.Context, in New) (*Service, error) {
	proto := in.Protocol
	if proto == "" {
		proto = "http"
	}
	row := r.pool.QueryRow(ctx, `
		INSERT INTO services(tenant_id, name, protocol, status)
		VALUES($1, $2, $3, 'active')
		RETURNING `+cols,
		in.TenantID, in.Name, proto)
	s, err := scanService(row)
	if err != nil {
		if pkg.IsUniqueViolation(err) {
			return nil, pkg.ErrConflict("服务名在该租户下已存在")
		}
		return nil, fmt.Errorf("insert service: %w", err)
	}
	return s, nil
}

// GetByID 查询。
func (r *Repository) GetByID(ctx context.Context, id uuid.UUID) (*Service, error) {
	s, err := scanService(r.pool.QueryRow(ctx, `SELECT `+cols+` FROM services WHERE id=$1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, pkg.ErrNotFound("服务不存在")
	}
	if err != nil {
		return nil, fmt.Errorf("get service: %w", err)
	}
	return s, nil
}

// ListByTenant 列出某租户服务。
func (r *Repository) ListByTenant(ctx context.Context, tenantID uuid.UUID) ([]*Service, error) {
	rows, err := r.pool.Query(ctx, `SELECT `+cols+` FROM services WHERE tenant_id=$1 ORDER BY name`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("list services: %w", err)
	}
	defer rows.Close()
	out := []*Service{}
	for rows.Next() {
		s, err := scanService(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// All 返回全部服务（路由缓存构建用）。
func (r *Repository) All(ctx context.Context) ([]*Service, error) {
	rows, err := r.pool.Query(ctx, `SELECT `+cols+` FROM services`)
	if err != nil {
		return nil, fmt.Errorf("all services: %w", err)
	}
	defer rows.Close()
	out := []*Service{}
	for rows.Next() {
		s, err := scanService(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// List 全量（分页）。
func (r *Repository) List(ctx context.Context, limit, offset int) ([]*Service, int, error) {
	var total int
	if err := r.pool.QueryRow(ctx, `SELECT count(*) FROM services`).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := r.pool.Query(ctx, `SELECT `+cols+` FROM services ORDER BY created_at DESC LIMIT $1 OFFSET $2`, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []*Service{}
	for rows.Next() {
		s, err := scanService(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, s)
	}
	return out, total, rows.Err()
}

// Update 应用非空更新。
func (r *Repository) Update(ctx context.Context, id uuid.UUID, in Update) (*Service, error) {
	s, err := r.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if in.Name != nil {
		s.Name = *in.Name
	}
	if in.Protocol != nil {
		s.Protocol = *in.Protocol
	}
	if in.Status != nil {
		s.Status = *in.Status
	}
	err = r.pool.QueryRow(ctx, `
		UPDATE services SET name=$2, protocol=$3, status=$4, updated_at=now()
		WHERE id=$1 RETURNING `+cols,
		id, s.Name, s.Protocol, s.Status).Scan(&s.ID, &s.TenantID, &s.Name, &s.Protocol, &s.Status, &s.CreatedAt, &s.UpdatedAt)
	if err != nil {
		if pkg.IsUniqueViolation(err) {
			return nil, pkg.ErrConflict("服务名在该租户下已存在")
		}
		return nil, fmt.Errorf("update service: %w", err)
	}
	return s, nil
}

// Delete 删除服务。有关联实例时拒绝（数据完整性由 service 层确保）。
func (r *Repository) Delete(ctx context.Context, id uuid.UUID) error {
	tag, err := r.pool.Exec(ctx, `DELETE FROM services WHERE id=$1`, id)
	if err != nil {
		if pkg.IsForeignKeyViolation(err) {
			return pkg.ErrConflict("服务下仍有实例，无法删除")
		}
		return fmt.Errorf("delete service: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return pkg.ErrNotFound("服务不存在")
	}
	return nil
}
