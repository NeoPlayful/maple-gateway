package project

import (
	"context"
	"fmt"
	"time"

	"entgo.io/ent/dialect/sql"
	"github.com/NeoPlayful/maple-gateway/server/ent"
	entproject "github.com/NeoPlayful/maple-gateway/server/ent/project"
	"github.com/NeoPlayful/maple-gateway/server/pkg"
	"github.com/google/uuid"
)

// Repository 是 Project 数据访问层（基于 Ent）。
type Repository struct {
	ent *ent.Client
}

// NewRepository 构造。
func NewRepository(client *ent.Client) *Repository {
	return &Repository{ent: client}
}

func toModel(e *ent.Project) *Project {
	return &Project{
		ID:            e.ID,
		TenantID:      e.TenantID,
		TemplateID:    e.TemplateID,
		Name:          e.Name,
		Description:   e.Description,
		Status:        Status(e.Status),
		NodeID:        e.NodeID,
		ApplicationID: e.ApplicationID,
		CreatedAt:     e.CreatedAt,
		UpdatedAt:     e.UpdatedAt,
	}
}

// SetApplicationID 记录实例化生成的 Application ID（幂等键）。
func (r *Repository) SetApplicationID(ctx context.Context, id uuid.UUID, appID string) error {
	err := r.ent.Project.UpdateOneID(id).
		SetApplicationID(appID).
		SetUpdatedAt(time.Now()).
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("set project application_id: %w", err)
	}
	return nil
}

// Create 插入项目。同租户内项目标识唯一。name 必须通过路径段校验（作为目录第三段）。
func (r *Repository) Create(ctx context.Context, in New) (*Project, error) {
	if err := pkg.ValidatePathSegment("项目标识", in.Name); err != nil {
		return nil, err
	}
	if in.TemplateID == uuid.Nil {
		return nil, pkg.ErrValidation("请选择模板")
	}
	now := time.Now()
	e, err := r.ent.Project.Create().
		SetTenantID(in.TenantID).
		SetTemplateID(in.TemplateID).
		SetName(in.Name).
		SetDescription(in.Description).
		SetStatus(string(StatusActive)).
		SetCreatedAt(now).
		SetUpdatedAt(now).
		Save(ctx)
	if err != nil {
		if pkg.IsUniqueViolation(err) {
			return nil, pkg.ErrConflict("该租户下已存在同名项目")
		}
		if pkg.IsForeignKeyViolation(err) {
			return nil, pkg.ErrValidation("租户不存在")
		}
		return nil, fmt.Errorf("insert project: %w", err)
	}
	return toModel(e), nil
}

// Get 查询单个项目。
func (r *Repository) Get(ctx context.Context, id uuid.UUID) (*Project, error) {
	e, err := r.ent.Project.Get(ctx, id)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, pkg.ErrNotFound("项目不存在")
		}
		return nil, fmt.Errorf("get project: %w", err)
	}
	return toModel(e), nil
}

// List 分页列出项目，支持按租户筛选（nil 表示全部）。
func (r *Repository) List(ctx context.Context, tenantID *uuid.UUID, limit, offset int) ([]*Project, int, error) {
	q := r.ent.Project.Query()
	if tenantID != nil {
		q = q.Where(entproject.TenantID(*tenantID))
	}
	total, err := q.Count(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("count projects: %w", err)
	}
	es, err := q.
		Order(entproject.ByCreatedAt(sql.OrderDesc())).
		Limit(limit).
		Offset(offset).
		All(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("list projects: %w", err)
	}
	out := make([]*Project, 0, len(es))
	for _, e := range es {
		out = append(out, toModel(e))
	}
	return out, total, nil
}

// Update 应用非空更新（name/tenant/template 不可改）。
func (r *Repository) Update(ctx context.Context, id uuid.UUID, in Update) (*Project, error) {
	if _, err := r.ent.Project.Get(ctx, id); err != nil {
		if ent.IsNotFound(err) {
			return nil, pkg.ErrNotFound("项目不存在")
		}
		return nil, fmt.Errorf("get project for update: %w", err)
	}
	upd := r.ent.Project.UpdateOneID(id).SetUpdatedAt(time.Now())
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
	if in.NodeID != nil {
		upd = upd.SetNodeID(*in.NodeID)
	}
	e, err := upd.Save(ctx)
	if err != nil {
		return nil, fmt.Errorf("update project: %w", err)
	}
	return toModel(e), nil
}

// SetStatus 便捷状态变更。
func (r *Repository) SetStatus(ctx context.Context, id uuid.UUID, s Status) (*Project, error) {
	return r.Update(ctx, id, Update{Status: &s})
}

// Delete 删除项目。
func (r *Repository) Delete(ctx context.Context, id uuid.UUID) error {
	err := r.ent.Project.DeleteOneID(id).Exec(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return pkg.ErrNotFound("项目不存在")
		}
		return fmt.Errorf("delete project: %w", err)
	}
	return nil
}
