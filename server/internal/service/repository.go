package service

import (
	"context"
	"fmt"
	"time"

	"entgo.io/ent/dialect/sql"
	"github.com/NeoPlayful/maple-gateway/server/ent"
	entservice "github.com/NeoPlayful/maple-gateway/server/ent/service"
	"github.com/NeoPlayful/maple-gateway/server/pkg"
	"github.com/google/uuid"
)

// Repository 是 Service 数据访问层（基于 Ent）。
type Repository struct {
	ent *ent.Client
}

// NewRepository 构造。
func NewRepository(client *ent.Client) *Repository {
	return &Repository{ent: client}
}

// toModel 把 Ent 实体映射为领域模型。
func toModel(e *ent.Service) *Service {
	return &Service{
		ID:        e.ID,
		TenantID:  e.TenantID,
		Name:      e.Name,
		Protocol:  e.Protocol,
		Status:    Status(e.Status),
		CreatedAt: e.CreatedAt,
		UpdatedAt: e.UpdatedAt,
	}
}

// Create 插入。tenant 内 name 冲突返回 Conflict。
func (r *Repository) Create(ctx context.Context, in New) (*Service, error) {
	proto := in.Protocol
	if proto == "" {
		proto = "http"
	}
	now := time.Now()
	e, err := r.ent.Service.Create().
		SetTenantID(in.TenantID).
		SetName(in.Name).
		SetProtocol(proto).
		SetStatus(string(StatusActive)).
		SetCreatedAt(now).
		SetUpdatedAt(now).
		Save(ctx)
	if err != nil {
		if pkg.IsUniqueViolation(err) {
			return nil, pkg.ErrConflict("服务名在该租户下已存在")
		}
		return nil, fmt.Errorf("insert service: %w", err)
	}
	return toModel(e), nil
}

// GetByID 查询。
func (r *Repository) GetByID(ctx context.Context, id uuid.UUID) (*Service, error) {
	e, err := r.ent.Service.Get(ctx, id)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, pkg.ErrNotFound("服务不存在")
		}
		return nil, fmt.Errorf("get service: %w", err)
	}
	return toModel(e), nil
}

// ListByTenant 列出某租户服务。
func (r *Repository) ListByTenant(ctx context.Context, tenantID uuid.UUID) ([]*Service, error) {
	es, err := r.ent.Service.Query().
		Where(entservice.TenantID(tenantID)).
		Order(entservice.ByName()).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("list services: %w", err)
	}
	out := make([]*Service, 0, len(es))
	for _, e := range es {
		out = append(out, toModel(e))
	}
	return out, nil
}

// All 返回全部服务（路由缓存构建用）。
func (r *Repository) All(ctx context.Context) ([]*Service, error) {
	es, err := r.ent.Service.Query().All(ctx)
	if err != nil {
		return nil, fmt.Errorf("all services: %w", err)
	}
	out := make([]*Service, 0, len(es))
	for _, e := range es {
		out = append(out, toModel(e))
	}
	return out, nil
}

// List 全量（分页）。
func (r *Repository) List(ctx context.Context, limit, offset int) ([]*Service, int, error) {
	total, err := r.ent.Service.Query().Count(ctx)
	if err != nil {
		return nil, 0, err
	}
	es, err := r.ent.Service.Query().
		Order(entservice.ByCreatedAt(sql.OrderDesc())).
		Limit(limit).
		Offset(offset).
		All(ctx)
	if err != nil {
		return nil, 0, err
	}
	out := make([]*Service, 0, len(es))
	for _, e := range es {
		out = append(out, toModel(e))
	}
	return out, total, nil
}

// Update 应用非空更新。
func (r *Repository) Update(ctx context.Context, id uuid.UUID, in Update) (*Service, error) {
	if _, err := r.ent.Service.Get(ctx, id); err != nil {
		if ent.IsNotFound(err) {
			return nil, pkg.ErrNotFound("服务不存在")
		}
		return nil, fmt.Errorf("get service for update: %w", err)
	}

	upd := r.ent.Service.UpdateOneID(id).SetUpdatedAt(time.Now())
	if in.Name != nil {
		upd = upd.SetName(*in.Name)
	}
	if in.Protocol != nil {
		upd = upd.SetProtocol(*in.Protocol)
	}
	if in.Status != nil {
		upd = upd.SetStatus(string(*in.Status))
	}
	e, err := upd.Save(ctx)
	if err != nil {
		if pkg.IsUniqueViolation(err) {
			return nil, pkg.ErrConflict("服务名在该租户下已存在")
		}
		return nil, fmt.Errorf("update service: %w", err)
	}
	return toModel(e), nil
}

// Delete 删除服务。有关联实例时拒绝（数据完整性由 service 层确保）。
func (r *Repository) Delete(ctx context.Context, id uuid.UUID) error {
	err := r.ent.Service.DeleteOneID(id).Exec(ctx)
	if err != nil {
		if pkg.IsForeignKeyViolation(err) {
			return pkg.ErrConflict("服务下仍有实例，无法删除")
		}
		if ent.IsNotFound(err) {
			return pkg.ErrNotFound("服务不存在")
		}
		return fmt.Errorf("delete service: %w", err)
	}
	return nil
}
