package domain

import (
	"context"
	"fmt"
	"time"

	"github.com/NeoPlayful/maple-gateway/server/ent"
	entdomain "github.com/NeoPlayful/maple-gateway/server/ent/domain"
	"github.com/NeoPlayful/maple-gateway/server/pkg"
	"github.com/google/uuid"
)

// Repository 是 Domain 数据访问层（基于 Ent）。
type Repository struct {
	ent *ent.Client
}

// NewRepository 构造。
func NewRepository(client *ent.Client) *Repository {
	return &Repository{ent: client}
}

// toModel 把 Ent 实体映射为领域模型。
func toModel(e *ent.Domain) *Domain {
	return &Domain{
		ID:         e.ID,
		TenantID:   e.TenantID,
		Hostname:   e.Hostname,
		ServiceID:  e.ServiceID,
		Status:     Status(e.Status),
		VerifiedAt: e.VerifiedAt,
		CreatedAt:  e.CreatedAt,
		UpdatedAt:  e.UpdatedAt,
	}
}

// Create 插入新域名。hostname 冲突返回 Conflict。
func (r *Repository) Create(ctx context.Context, in New) (*Domain, error) {
	now := time.Now()
	e, err := r.ent.Domain.Create().
		SetTenantID(in.TenantID).
		SetHostname(in.Hostname).
		SetNillableServiceID(in.ServiceID).
		SetStatus(string(StatusActive)).
		SetCreatedAt(now).
		SetUpdatedAt(now).
		Save(ctx)
	if err != nil {
		if pkg.IsUniqueViolation(err) {
			return nil, pkg.ErrConflict("hostname 已存在")
		}
		return nil, fmt.Errorf("insert domain: %w", err)
	}
	return toModel(e), nil
}

// GetByID 查询。
func (r *Repository) GetByID(ctx context.Context, id uuid.UUID) (*Domain, error) {
	e, err := r.ent.Domain.Get(ctx, id)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, pkg.ErrNotFound("域名不存在")
		}
		return nil, fmt.Errorf("get domain: %w", err)
	}
	return toModel(e), nil
}

// GetByHostname 查询（数据平面路由用）。
func (r *Repository) GetByHostname(ctx context.Context, hostname string) (*Domain, error) {
	e, err := r.ent.Domain.Query().
		Where(entdomain.HostnameEQ(hostname)).
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, pkg.ErrNotFound("域名不存在")
		}
		return nil, fmt.Errorf("get domain by hostname: %w", err)
	}
	return toModel(e), nil
}

// ListByTenant 列出某租户的域名。
func (r *Repository) ListByTenant(ctx context.Context, tenantID uuid.UUID) ([]*Domain, error) {
	es, err := r.ent.Domain.Query().
		Where(entdomain.TenantID(tenantID)).
		Order(entdomain.ByHostname()).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("list domains: %w", err)
	}
	out := make([]*Domain, 0, len(es))
	for _, e := range es {
		out = append(out, toModel(e))
	}
	return out, nil
}

// All 返回全部域名（路由缓存构建用）。
func (r *Repository) All(ctx context.Context) ([]*Domain, error) {
	es, err := r.ent.Domain.Query().Order(entdomain.ByHostname()).All(ctx)
	if err != nil {
		return nil, fmt.Errorf("all domains: %w", err)
	}
	out := make([]*Domain, 0, len(es))
	for _, e := range es {
		out = append(out, toModel(e))
	}
	return out, nil
}

// List 全量域名（分页）。
func (r *Repository) List(ctx context.Context, limit, offset int) ([]*Domain, int, error) {
	total, err := r.ent.Domain.Query().Count(ctx)
	if err != nil {
		return nil, 0, err
	}
	es, err := r.ent.Domain.Query().
		Order(entdomain.ByHostname()).
		Limit(limit).
		Offset(offset).
		All(ctx)
	if err != nil {
		return nil, 0, err
	}
	out := make([]*Domain, 0, len(es))
	for _, e := range es {
		out = append(out, toModel(e))
	}
	return out, total, nil
}

// Update 应用非空更新。
func (r *Repository) Update(ctx context.Context, id uuid.UUID, in Update) (*Domain, error) {
	if _, err := r.ent.Domain.Get(ctx, id); err != nil {
		if ent.IsNotFound(err) {
			return nil, pkg.ErrNotFound("域名不存在")
		}
		return nil, fmt.Errorf("get domain for update: %w", err)
	}

	upd := r.ent.Domain.UpdateOneID(id).SetUpdatedAt(time.Now())
	if in.Hostname != nil {
		upd = upd.SetHostname(*in.Hostname)
	}
	if in.ServiceID != nil {
		upd = upd.SetServiceID(*in.ServiceID)
	} else {
		// 显式清空默认 service 引用。
		upd = upd.ClearServiceID()
	}
	if in.Status != nil {
		upd = upd.SetStatus(string(*in.Status))
	}
	e, err := upd.Save(ctx)
	if err != nil {
		if pkg.IsUniqueViolation(err) {
			return nil, pkg.ErrConflict("hostname 已存在")
		}
		return nil, fmt.Errorf("update domain: %w", err)
	}
	return toModel(e), nil
}

// Delete 删除。
func (r *Repository) Delete(ctx context.Context, id uuid.UUID) error {
	err := r.ent.Domain.DeleteOneID(id).Exec(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return pkg.ErrNotFound("域名不存在")
		}
		return fmt.Errorf("delete domain: %w", err)
	}
	return nil
}
