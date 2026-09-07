package ratelimit

import (
	"context"
	"errors"
	"fmt"

	"github.com/NeoPlayful/maple-gateway/server/pkg"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Repository 是限流规则数据访问层。
type Repository struct {
	pool *pgxpool.Pool
}

// NewRepository 构造。
func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

const cols = `id, scope, tenant_id, domain_id, service_id, name, "limit", window_seconds,
	burst, response_code, status, created_at, updated_at`

func scanRL(row pgx.Row) (*RateLimit, error) {
	var r RateLimit
	err := row.Scan(&r.ID, &r.Scope, &r.TenantID, &r.DomainID, &r.ServiceID, &r.Name,
		&r.Limit, &r.WindowSeconds, &r.Burst, &r.ResponseCode, &r.Status,
		&r.CreatedAt, &r.UpdatedAt)
	if err != nil {
		return nil, err
	}
	if r.Burst == 0 {
		r.Burst = r.Limit // 缺省 burst = limit（令牌桶上限）
	}
	return &r, nil
}

func errNoRows(err error, msg string) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return pkg.ErrNotFound(msg)
	}
	return err
}

// validateScopeRefs 校验非 global scope 需带对应 ID。
func validateScopeRefs(in NewRateLimit) error {
	switch in.Scope {
	case ScopeTenant:
		if in.TenantID == nil {
			return pkg.ErrValidation("tenant scope 需要 tenant_id")
		}
	case ScopeDomain:
		if in.DomainID == nil {
			return pkg.ErrValidation("domain scope 需要 domain_id")
		}
	case ScopeService:
		if in.ServiceID == nil {
			return pkg.ErrValidation("service scope 需要 service_id")
		}
	}
	return nil
}

// Create 新增限流规则。同 scope+key 重复返回 Conflict。
func (r *Repository) Create(ctx context.Context, in NewRateLimit) (*RateLimit, error) {
	if err := validateScopeRefs(in); err != nil {
		return nil, err
	}
	code := in.ResponseCode
	if code == 0 {
		code = 429
	}
	row := r.pool.QueryRow(ctx, `
		INSERT INTO rate_limits(scope, tenant_id, domain_id, service_id, name,
			"limit", window_seconds, burst, response_code, status)
		VALUES($1, $2, $3, $4, $5, $6, $7, $8, $9, 'enabled')
		RETURNING `+cols,
		string(in.Scope), in.TenantID, in.DomainID, in.ServiceID, in.Name,
		in.Limit, in.WindowSeconds, in.Burst, code)
	rl, err := scanRL(row)
	if err != nil {
		if pkg.IsUniqueViolation(err) {
			return nil, pkg.ErrConflict("同维度下已存在同名限流规则")
		}
		if pkg.IsForeignKeyViolation(err) {
			return nil, pkg.ErrValidation("tenant/domain/service 不存在")
		}
		return nil, fmt.Errorf("insert rate limit: %w", err)
	}
	return rl, nil
}

// Get 查询规则。
func (r *Repository) Get(ctx context.Context, id uuid.UUID) (*RateLimit, error) {
	rl, err := scanRL(r.pool.QueryRow(ctx, `SELECT `+cols+` FROM rate_limits WHERE id=$1`, id))
	if e := errNoRows(err, "限流规则不存在"); e != nil {
		return nil, e
	}
	if err != nil {
		return nil, fmt.Errorf("get rate limit: %w", err)
	}
	return rl, nil
}

// List 分页列出，支持 scope 筛选。
func (r *Repository) List(ctx context.Context, scope Scope, limit, offset int) ([]*RateLimit, int, error) {
	var total int
	if err := r.pool.QueryRow(ctx,
		`SELECT count(*) FROM rate_limits WHERE ($1='' OR scope=$1)`, string(scope)).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count rate limits: %w", err)
	}
	rows, err := r.pool.Query(ctx, `
		SELECT `+cols+` FROM rate_limits
		WHERE ($1='' OR scope=$1)
		ORDER BY created_at DESC LIMIT $2 OFFSET $3`, string(scope), limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("list rate limits: %w", err)
	}
	defer rows.Close()
	out := []*RateLimit{}
	for rows.Next() {
		rl, err := scanRL(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, rl)
	}
	return out, total, rows.Err()
}

// All 返回全部 enabled 规则（路由表限流加载用，按优先级 global→ip 排序）。
func (r *Repository) All(ctx context.Context) ([]*RateLimit, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT `+cols+` FROM rate_limits WHERE status='enabled'
		ORDER BY created_at`)
	if err != nil {
		return nil, fmt.Errorf("all rate limits: %w", err)
	}
	defer rows.Close()
	out := []*RateLimit{}
	for rows.Next() {
		rl, err := scanRL(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, rl)
	}
	return out, rows.Err()
}

// Update 应用非空更新。
func (r *Repository) Update(ctx context.Context, id uuid.UUID, in UpdateRateLimit) (*RateLimit, error) {
	rl, err := r.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if in.Name != nil {
		rl.Name = *in.Name
	}
	if in.Limit != nil {
		rl.Limit = *in.Limit
	}
	if in.WindowSeconds != nil {
		rl.WindowSeconds = *in.WindowSeconds
	}
	if in.Burst != nil {
		rl.Burst = *in.Burst
	}
	if in.ResponseCode != nil {
		rl.ResponseCode = *in.ResponseCode
	}
	if in.Status != nil {
		rl.Status = *in.Status
	}
	upd, err := scanRL(r.pool.QueryRow(ctx, `
		UPDATE rate_limits SET name=$2, "limit"=$3, window_seconds=$4, burst=$5,
			response_code=$6, status=$7, updated_at=now()
		WHERE id=$1 RETURNING `+cols,
		id, rl.Name, rl.Limit, rl.WindowSeconds, rl.Burst, rl.ResponseCode, rl.Status))
	if err != nil {
		return nil, fmt.Errorf("update rate limit: %w", err)
	}
	return upd, nil
}

// SetStatus 便捷启停。
func (r *Repository) SetStatus(ctx context.Context, id uuid.UUID, s Status) (*RateLimit, error) {
	return r.Update(ctx, id, UpdateRateLimit{Status: &s})
}

// Delete 删除规则。
func (r *Repository) Delete(ctx context.Context, id uuid.UUID) error {
	tag, err := r.pool.Exec(ctx, `DELETE FROM rate_limits WHERE id=$1`, id)
	if err != nil {
		return fmt.Errorf("delete rate limit: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return pkg.ErrNotFound("限流规则不存在")
	}
	return nil
}
