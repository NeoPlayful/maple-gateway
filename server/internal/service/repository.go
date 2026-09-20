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
		ProjectID: e.ProjectID,
		Name:      e.Name,
		Protocol:  e.Protocol,
		Status:    Status(e.Status),
		CreatedAt: e.CreatedAt,
		UpdatedAt: e.UpdatedAt,
	}
}

// Create 在指定项目下插入服务。tenant_id 从项目继承，服务与项目一一对应。
// 经 ent 直接读项目（不依赖 project 包，避免与 project→service 的依赖成环）。
func (r *Repository) Create(ctx context.Context, in New) (*Service, error) {
	if in.ProjectID == uuid.Nil {
		return nil, pkg.ErrValidation("请选择项目")
	}
	proj, err := r.ent.Project.Get(ctx, in.ProjectID)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, pkg.ErrValidation("项目不存在")
		}
		return nil, fmt.Errorf("get project for service: %w", err)
	}
	proto := in.Protocol
	if proto == "" {
		proto = "http"
	}
	now := time.Now()
	e, err := r.ent.Service.Create().
		SetTenantID(proj.TenantID).
		SetProjectID(in.ProjectID).
		SetName(in.Name).
		SetProtocol(proto).
		SetStatus(string(StatusActive)).
		SetCreatedAt(now).
		SetUpdatedAt(now).
		Save(ctx)
	if err != nil {
		if pkg.IsUniqueViolation(err) {
			return nil, pkg.ErrConflict("该项目已存在服务，或服务名在该租户下已存在")
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

// ProjectIDForService 返回服务所属项目 ID；服务无归属时返回 nil。供部署下发派生出容器的项目归属。
func (r *Repository) ProjectIDForService(ctx context.Context, serviceID uuid.UUID) (*uuid.UUID, error) {
	s, err := r.GetByID(ctx, serviceID)
	if err != nil {
		return nil, err
	}
	return s.ProjectID, nil
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

// ListByProject 列出某项目的服务（一项目一服务，故至多一条）。
func (r *Repository) ListByProject(ctx context.Context, projectID uuid.UUID) ([]*Service, error) {
	es, err := r.ent.Service.Query().
		Where(entservice.ProjectID(projectID)).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("list services by project: %w", err)
	}
	out := make([]*Service, 0, len(es))
	for _, e := range es {
		out = append(out, toModel(e))
	}
	return out, nil
}

// EnsureForProject 返回项目的服务，不存在则以 name 创建（幂等）。一项目一服务。
func (r *Repository) EnsureForProject(ctx context.Context, projectID uuid.UUID, name string) (uuid.UUID, error) {
	if es, err := r.ListByProject(ctx, projectID); err != nil {
		return uuid.Nil, err
	} else if len(es) > 0 {
		return es[0].ID, nil
	}
	s, err := r.Create(ctx, New{ProjectID: projectID, Name: name})
	if err != nil {
		return uuid.Nil, err
	}
	return s.ID, nil
}

// ServiceForProject 返回项目已绑定的服务 ID；未绑定返回 uuid.Nil, false。
func (r *Repository) ServiceForProject(ctx context.Context, projectID uuid.UUID) (uuid.UUID, bool, error) {
	es, err := r.ListByProject(ctx, projectID)
	if err != nil {
		return uuid.Nil, false, err
	}
	if len(es) == 0 {
		return uuid.Nil, false, nil
	}
	return es[0].ID, true, nil
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
	if in.ProjectID != nil {
		// 调整归属：连带继承新项目的 tenant_id，保持派生字段一致。
		proj, err := r.ent.Project.Get(ctx, *in.ProjectID)
		if err != nil {
			if ent.IsNotFound(err) {
				return nil, pkg.ErrValidation("项目不存在")
			}
			return nil, fmt.Errorf("get project for service update: %w", err)
		}
		upd = upd.SetProjectID(*in.ProjectID).SetTenantID(proj.TenantID)
	}
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
			return nil, pkg.ErrConflict("该项目已存在服务，或服务名在该租户下已存在")
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
