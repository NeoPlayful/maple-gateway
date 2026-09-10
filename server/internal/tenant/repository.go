package tenant

import (
	"context"
	"fmt"
	"time"

	"entgo.io/ent/dialect/sql"
	"github.com/NeoPlayful/maple-gateway/server/ent"
	enttenant "github.com/NeoPlayful/maple-gateway/server/ent/tenant"
	"github.com/NeoPlayful/maple-gateway/server/pkg"
	"github.com/google/uuid"
)

// Repository 是 Tenant 数据访问层（基于 Ent）。
type Repository struct {
	ent *ent.Client
}

// NewRepository 构造。
func NewRepository(client *ent.Client) *Repository {
	return &Repository{ent: client}
}

// toModel 把 Ent 实体映射为领域模型。
func toModel(e *ent.Tenant) *Tenant {
	return &Tenant{
		ID:          e.ID,
		Name:        e.Name,
		Slug:        e.Slug,
		Status:      Status(e.Status),
		Description: e.Description,
		CreatedAt:   e.CreatedAt,
		UpdatedAt:   e.UpdatedAt,
	}
}

// Create 插入新租户。slug 冲突返回 Conflict。
func (r *Repository) Create(ctx context.Context, in New) (*Tenant, error) {
	now := time.Now()
	e, err := r.ent.Tenant.Create().
		SetName(in.Name).
		SetSlug(in.Slug).
		SetStatus(string(StatusActive)).
		SetDescription(in.Description).
		SetCreatedAt(now).
		SetUpdatedAt(now).
		Save(ctx)
	if err != nil {
		if pkg.IsUniqueViolation(err) {
			return nil, pkg.ErrConflict("slug 已存在")
		}
		return nil, fmt.Errorf("insert tenant: %w", err)
	}
	return toModel(e), nil
}

// GetByID 查询单个租户。
func (r *Repository) GetByID(ctx context.Context, id uuid.UUID) (*Tenant, error) {
	e, err := r.ent.Tenant.Get(ctx, id)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, pkg.ErrNotFound("租户不存在")
		}
		return nil, fmt.Errorf("get tenant: %w", err)
	}
	return toModel(e), nil
}

// All 返回全部租户（路由缓存构建用）。
func (r *Repository) All(ctx context.Context) ([]*Tenant, error) {
	es, err := r.ent.Tenant.Query().All(ctx)
	if err != nil {
		return nil, fmt.Errorf("all tenants: %w", err)
	}
	out := make([]*Tenant, 0, len(es))
	for _, e := range es {
		out = append(out, toModel(e))
	}
	return out, nil
}

// List 分页列出租户，支持按状态筛选。
func (r *Repository) List(ctx context.Context, status Status, limit, offset int) ([]*Tenant, int, error) {
	q := r.ent.Tenant.Query()
	if status != "" {
		q = q.Where(enttenant.StatusEQ(string(status)))
	}
	total, err := q.Count(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("count tenants: %w", err)
	}
	es, err := q.
		Order(enttenant.ByCreatedAt(sql.OrderDesc())).
		Limit(limit).
		Offset(offset).
		All(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("list tenants: %w", err)
	}
	out := make([]*Tenant, 0, len(es))
	for _, e := range es {
		out = append(out, toModel(e))
	}
	return out, total, nil
}

// Update 应用非空更新。
func (r *Repository) Update(ctx context.Context, id uuid.UUID, in Update) (*Tenant, error) {
	// 不存在则直接 NotFound（避免空更新返回 success）。
	if _, err := r.ent.Tenant.Get(ctx, id); err != nil {
		if ent.IsNotFound(err) {
			return nil, pkg.ErrNotFound("租户不存在")
		}
		return nil, fmt.Errorf("get tenant for update: %w", err)
	}

	upd := r.ent.Tenant.UpdateOneID(id).SetUpdatedAt(time.Now())
	if in.Name != nil {
		upd = upd.SetName(*in.Name)
	}
	// description 三态：设置 / 显式清空 / 保持不变（默认）。
	switch {
	case in.Description != nil:
		upd = upd.SetDescription(*in.Description)
	case in.ClearDescription:
		upd = upd.SetDescription("")
	}
	if in.Status != nil {
		upd = upd.SetStatus(string(*in.Status))
	}
	e, err := upd.Save(ctx)
	if err != nil {
		return nil, fmt.Errorf("update tenant: %w", err)
	}
	return toModel(e), nil
}

// SetStatus 便捷状态变更。
func (r *Repository) SetStatus(ctx context.Context, id uuid.UUID, s Status) (*Tenant, error) {
	return r.Update(ctx, id, Update{Status: &s})
}

// Delete 删除租户。
func (r *Repository) Delete(ctx context.Context, id uuid.UUID) error {
	err := r.ent.Tenant.DeleteOneID(id).Exec(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return pkg.ErrNotFound("租户不存在")
		}
		return fmt.Errorf("delete tenant: %w", err)
	}
	return nil
}
