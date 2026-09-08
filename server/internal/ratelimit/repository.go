package ratelimit

import (
	"context"
	"fmt"
	"time"

	"entgo.io/ent/dialect/sql"
	"github.com/NeoPlayful/maple-gateway/server/ent"
	entrl "github.com/NeoPlayful/maple-gateway/server/ent/ratelimit"
	"github.com/NeoPlayful/maple-gateway/server/pkg"
	"github.com/google/uuid"
)

// Repository 是限流规则数据访问层（基于 Ent）。
type Repository struct {
	ent *ent.Client
}

// NewRepository 构造。
func NewRepository(client *ent.Client) *Repository {
	return &Repository{ent: client}
}

func toModel(e *ent.RateLimit) *RateLimit {
	r := &RateLimit{
		ID:            e.ID,
		Scope:         Scope(e.Scope),
		TenantID:      e.TenantID,
		DomainID:      e.DomainID,
		ServiceID:     e.ServiceID,
		Name:          e.Name,
		Limit:         e.Limit,
		WindowSeconds: e.WindowSeconds,
		Burst:         e.Burst,
		ResponseCode:  e.ResponseCode,
		Status:        Status(e.Status),
		CreatedAt:     e.CreatedAt,
		UpdatedAt:     e.UpdatedAt,
	}
	if r.Burst == 0 {
		r.Burst = r.Limit // 缺省 burst = limit（令牌桶上限）
	}
	return r
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
	now := time.Now()
	cb := r.ent.RateLimit.Create().
		SetScope(string(in.Scope)).
		SetNillableTenantID(in.TenantID).
		SetNillableDomainID(in.DomainID).
		SetNillableServiceID(in.ServiceID).
		SetName(in.Name).
		SetLimit(in.Limit).
		SetWindowSeconds(in.WindowSeconds).
		SetBurst(in.Burst).
		SetResponseCode(code).
		SetStatus(string(StatusEnabled)).
		SetCreatedAt(now).
		SetUpdatedAt(now)
	e, err := cb.Save(ctx)
	if err != nil {
		if pkg.IsUniqueViolation(err) {
			return nil, pkg.ErrConflict("同维度下已存在同名限流规则")
		}
		if pkg.IsForeignKeyViolation(err) {
			return nil, pkg.ErrValidation("tenant/domain/service 不存在")
		}
		return nil, fmt.Errorf("insert rate limit: %w", err)
	}
	return toModel(e), nil
}

// Get 查询规则。
func (r *Repository) Get(ctx context.Context, id uuid.UUID) (*RateLimit, error) {
	e, err := r.ent.RateLimit.Get(ctx, id)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, pkg.ErrNotFound("限流规则不存在")
		}
		return nil, fmt.Errorf("get rate limit: %w", err)
	}
	return toModel(e), nil
}

// List 分页列出，支持 scope 筛选。
func (r *Repository) List(ctx context.Context, scope Scope, limit, offset int) ([]*RateLimit, int, error) {
	q := r.ent.RateLimit.Query()
	if scope != "" {
		q = q.Where(entrl.ScopeEQ(string(scope)))
	}
	total, err := q.Count(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("count rate limits: %w", err)
	}
	es, err := q.
		Order(entrl.ByCreatedAt(sql.OrderDesc())).
		Limit(limit).
		Offset(offset).
		All(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("list rate limits: %w", err)
	}
	out := make([]*RateLimit, 0, len(es))
	for _, e := range es {
		out = append(out, toModel(e))
	}
	return out, total, nil
}

// All 返回全部 enabled 规则（路由表限流加载用）。
func (r *Repository) All(ctx context.Context) ([]*RateLimit, error) {
	es, err := r.ent.RateLimit.Query().
		Where(entrl.StatusEQ(string(StatusEnabled))).
		Order(entrl.ByCreatedAt()).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("all rate limits: %w", err)
	}
	out := make([]*RateLimit, 0, len(es))
	for _, e := range es {
		out = append(out, toModel(e))
	}
	return out, nil
}

// Update 应用非空更新。
func (r *Repository) Update(ctx context.Context, id uuid.UUID, in UpdateRateLimit) (*RateLimit, error) {
	if _, err := r.ent.RateLimit.Get(ctx, id); err != nil {
		if ent.IsNotFound(err) {
			return nil, pkg.ErrNotFound("限流规则不存在")
		}
		return nil, fmt.Errorf("get rate limit for update: %w", err)
	}
	upd := r.ent.RateLimit.UpdateOneID(id).SetUpdatedAt(time.Now())
	if in.Name != nil {
		upd = upd.SetName(*in.Name)
	}
	if in.Limit != nil {
		upd = upd.SetLimit(*in.Limit)
	}
	if in.WindowSeconds != nil {
		upd = upd.SetWindowSeconds(*in.WindowSeconds)
	}
	if in.Burst != nil {
		upd = upd.SetBurst(*in.Burst)
	}
	if in.ResponseCode != nil {
		upd = upd.SetResponseCode(*in.ResponseCode)
	}
	if in.Status != nil {
		upd = upd.SetStatus(string(*in.Status))
	}
	e, err := upd.Save(ctx)
	if err != nil {
		return nil, fmt.Errorf("update rate limit: %w", err)
	}
	return toModel(e), nil
}

// SetStatus 便捷启停。
func (r *Repository) SetStatus(ctx context.Context, id uuid.UUID, s Status) (*RateLimit, error) {
	return r.Update(ctx, id, UpdateRateLimit{Status: &s})
}

// Delete 删除规则。
func (r *Repository) Delete(ctx context.Context, id uuid.UUID) error {
	err := r.ent.RateLimit.DeleteOneID(id).Exec(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return pkg.ErrNotFound("限流规则不存在")
		}
		return fmt.Errorf("delete rate limit: %w", err)
	}
	return nil
}
