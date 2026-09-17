package apptemplate

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"entgo.io/ent/dialect/sql"
	"github.com/NeoPlayful/maple-gateway/server/ent"
	enttemplate "github.com/NeoPlayful/maple-gateway/server/ent/template"
	"github.com/NeoPlayful/maple-gateway/server/pkg"
	"github.com/google/uuid"
)

// Repository 是 Template 数据访问层（基于 Ent）。
type Repository struct {
	ent *ent.Client
}

// NewRepository 构造。
func NewRepository(client *ent.Client) *Repository {
	return &Repository{ent: client}
}

func toModel(e *ent.Template) *Template {
	return &Template{
		ID:          e.ID,
		Name:        e.Name,
		Slug:        e.Slug,
		Description: e.Description,
		Spec:        e.Spec,
		Params:      decodeParams(e.Params),
		Status:      Status(e.Status),
		CreatedAt:   e.CreatedAt,
		UpdatedAt:   e.UpdatedAt,
	}
}

// decodeParams 解析参数定义 JSONB；空或非法返回 nil。
func decodeParams(raw json.RawMessage) []Param {
	if len(raw) == 0 {
		return nil
	}
	var ps []Param
	if err := json.Unmarshal(raw, &ps); err != nil {
		return nil
	}
	return ps
}

// Create 插入模板。slug 冲突返回 Conflict；slug 作为路径段受字符集约束。
func (r *Repository) Create(ctx context.Context, in New) (*Template, error) {
	if err := pkg.ValidatePathSegment("模板标识", in.Slug); err != nil {
		return nil, err
	}
	if err := ValidateParams(in.Params); err != nil {
		return nil, err
	}
	now := time.Now()
	c := r.ent.Template.Create().
		SetName(in.Name).
		SetSlug(in.Slug).
		SetDescription(in.Description).
		SetSpec(in.Spec).
		SetStatus(string(StatusActive)).
		SetCreatedAt(now).
		SetUpdatedAt(now)
	if raw := MarshalParams(in.Params); raw != nil {
		c = c.SetParams(raw)
	}
	e, err := c.Save(ctx)
	if err != nil {
		if pkg.IsUniqueViolation(err) {
			return nil, pkg.ErrConflict("模板标识已存在")
		}
		return nil, fmt.Errorf("insert template: %w", err)
	}
	return toModel(e), nil
}

// Get 查询单个模板。
func (r *Repository) Get(ctx context.Context, id uuid.UUID) (*Template, error) {
	e, err := r.ent.Template.Get(ctx, id)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, pkg.ErrNotFound("模板不存在")
		}
		return nil, fmt.Errorf("get template: %w", err)
	}
	return toModel(e), nil
}

// List 分页列出模板，支持按状态筛选。
func (r *Repository) List(ctx context.Context, status Status, limit, offset int) ([]*Template, int, error) {
	q := r.ent.Template.Query()
	if status != "" {
		q = q.Where(enttemplate.StatusEQ(string(status)))
	}
	total, err := q.Count(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("count templates: %w", err)
	}
	es, err := q.
		Order(enttemplate.ByCreatedAt(sql.OrderDesc())).
		Limit(limit).
		Offset(offset).
		All(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("list templates: %w", err)
	}
	out := make([]*Template, 0, len(es))
	for _, e := range es {
		out = append(out, toModel(e))
	}
	return out, total, nil
}

// Update 应用非空更新（slug 不可改）。
func (r *Repository) Update(ctx context.Context, id uuid.UUID, in Update) (*Template, error) {
	if _, err := r.ent.Template.Get(ctx, id); err != nil {
		if ent.IsNotFound(err) {
			return nil, pkg.ErrNotFound("模板不存在")
		}
		return nil, fmt.Errorf("get template for update: %w", err)
	}
	upd := r.ent.Template.UpdateOneID(id).SetUpdatedAt(time.Now())
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
	if in.Spec != nil {
		upd = upd.SetSpec(*in.Spec)
	}
	if in.Status != nil {
		upd = upd.SetStatus(string(*in.Status))
	}
	if in.Params != nil {
		if err := ValidateParams(*in.Params); err != nil {
			return nil, err
		}
		if raw := MarshalParams(*in.Params); raw != nil {
			upd = upd.SetParams(raw)
		} else {
			upd = upd.ClearParams()
		}
	}
	e, err := upd.Save(ctx)
	if err != nil {
		return nil, fmt.Errorf("update template: %w", err)
	}
	return toModel(e), nil
}

// SetStatus 便捷状态变更。
func (r *Repository) SetStatus(ctx context.Context, id uuid.UUID, s Status) (*Template, error) {
	return r.Update(ctx, id, Update{Status: &s})
}

// Delete 删除模板。
func (r *Repository) Delete(ctx context.Context, id uuid.UUID) error {
	err := r.ent.Template.DeleteOneID(id).Exec(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return pkg.ErrNotFound("模板不存在")
		}
		return fmt.Errorf("delete template: %w", err)
	}
	return nil
}
