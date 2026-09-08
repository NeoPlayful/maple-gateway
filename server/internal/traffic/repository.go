package traffic

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"entgo.io/ent/dialect/sql"
	"github.com/NeoPlayful/maple-gateway/server/ent"
	enttraffic "github.com/NeoPlayful/maple-gateway/server/ent/trafficpolicy"
	"github.com/NeoPlayful/maple-gateway/server/pkg"
	"github.com/google/uuid"
)

// NewInput 是策略创建输入。
type NewInput struct {
	ServiceID       uuid.UUID  `json:"service_id" validate:"required"`
	Name            string     `json:"name" validate:"required,min=1,max=64"`
	Priority        int        `json:"priority"`
	Match           *Match     `json:"match"`
	TargetVersionID *uuid.UUID `json:"target_version_id"`
	Weight          int        `json:"weight"`
	Sticky          *Sticky    `json:"sticky"`
}

// UpdateInput 是策略可修改字段。
type UpdateInput struct {
	Name            *string    `json:"name"`
	Priority        *int       `json:"priority"`
	Match           *Match     `json:"match"`
	TargetVersionID *uuid.UUID `json:"target_version_id"` // uuid.Nil 表示清空定向
	Weight          *int       `json:"weight"`
	Sticky          *Sticky    `json:"sticky"`
	Status          *Status    `json:"status"`
}

// Repository 是流量策略数据访问层（基于 Ent）。
type Repository struct {
	ent *ent.Client
}

// NewRepository 构造。
func NewRepository(client *ent.Client) *Repository {
	return &Repository{ent: client}
}

// fromEnt 把 Ent 实体映射为领域模型（解析 match/sticky JSONB）。
func fromEnt(e *ent.TrafficPolicy) (*Policy, error) {
	p := &Policy{
		ID:              e.ID,
		ServiceID:       e.ServiceID,
		Name:            e.Name,
		Priority:        e.Priority,
		TargetVersionID: e.TargetVersionID,
		Weight:          e.Weight,
		Status:          Status(e.Status),
		CreatedAt:       e.CreatedAt,
		UpdatedAt:       e.UpdatedAt,
	}
	m, err := NewMatch(e.Match)
	if err != nil {
		return nil, fmt.Errorf("parse policy %s match: %w", e.ID, err)
	}
	p.Match = m
	if len(e.Sticky) > 0 {
		s, err := NewSticky(e.Sticky)
		if err != nil {
			return nil, fmt.Errorf("parse policy %s sticky: %w", e.ID, err)
		}
		p.Sticky = s
	}
	return p, nil
}

func marshalMatch(m *Match) (json.RawMessage, error) {
	if m == nil {
		return json.Marshal(Match{})
	}
	return json.Marshal(m)
}

func marshalSticky(s *Sticky) (json.RawMessage, error) {
	if s == nil {
		return nil, nil
	}
	return json.Marshal(s)
}

// Create 新增策略。同 service 内同名冲突返回 Conflict。
func (r *Repository) Create(ctx context.Context, in NewInput) (*Policy, error) {
	matchRaw, err := marshalMatch(in.Match)
	if err != nil {
		return nil, err
	}
	stickyRaw, err := marshalSticky(in.Sticky)
	if err != nil {
		return nil, err
	}
	priority := in.Priority
	if priority == 0 {
		priority = 100
	}
	now := time.Now()
	cb := r.ent.TrafficPolicy.Create().
		SetServiceID(in.ServiceID).
		SetName(in.Name).
		SetPriority(priority).
		SetMatch(matchRaw).
		SetNillableTargetVersionID(in.TargetVersionID).
		SetWeight(in.Weight).
		SetStatus(string(StatusEnabled)).
		SetCreatedAt(now).
		SetUpdatedAt(now)
	if stickyRaw != nil {
		cb = cb.SetSticky(stickyRaw)
	}
	e, err := cb.Save(ctx)
	if err != nil {
		if pkg.IsUniqueViolation(err) {
			return nil, pkg.ErrConflict("该服务下已存在同名策略")
		}
		if pkg.IsForeignKeyViolation(err) {
			return nil, pkg.ErrValidation("service_id 或 target_version_id 不存在")
		}
		return nil, fmt.Errorf("insert traffic policy: %w", err)
	}
	return fromEnt(e)
}

// ListByService 列出某服务的策略（按 priority 升序）。
func (r *Repository) ListByService(ctx context.Context, serviceID uuid.UUID) ([]*Policy, error) {
	es, err := r.ent.TrafficPolicy.Query().
		Where(enttraffic.ServiceID(serviceID)).
		Order(enttraffic.ByPriority(), enttraffic.ByCreatedAt()).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("list traffic policies by service: %w", err)
	}
	return collectPolicies(es)
}

// ListAll 分页列出全部策略。
func (r *Repository) ListAll(ctx context.Context, limit, offset int) ([]*Policy, int, error) {
	total, err := r.ent.TrafficPolicy.Query().Count(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("count traffic policies: %w", err)
	}
	es, err := r.ent.TrafficPolicy.Query().
		Order(enttraffic.ByCreatedAt(sql.OrderDesc())).
		Limit(limit).
		Offset(offset).
		All(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("list traffic policies: %w", err)
	}
	out, err := collectPolicies(es)
	return out, total, err
}

// AllGroupedByService 返回全部策略按 service_id 分组（路由表构建用）。
func (r *Repository) AllGroupedByService(ctx context.Context) (map[uuid.UUID][]*Policy, error) {
	es, err := r.ent.TrafficPolicy.Query().
		Order(enttraffic.ByServiceID(), enttraffic.ByPriority()).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("all traffic policies grouped: %w", err)
	}
	out := map[uuid.UUID][]*Policy{}
	for _, e := range es {
		p, err := fromEnt(e)
		if err != nil {
			return nil, err
		}
		out[p.ServiceID] = append(out[p.ServiceID], p)
	}
	return out, nil
}

// GetByID 查询策略。
func (r *Repository) GetByID(ctx context.Context, id uuid.UUID) (*Policy, error) {
	e, err := r.ent.TrafficPolicy.Get(ctx, id)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, pkg.ErrNotFound("策略不存在")
		}
		return nil, fmt.Errorf("get traffic policy: %w", err)
	}
	return fromEnt(e)
}

// Update 应用非空更新。
func (r *Repository) Update(ctx context.Context, id uuid.UUID, in UpdateInput) (*Policy, error) {
	// 确认存在（不存在报 NotFound）。
	if _, err := r.GetByID(ctx, id); err != nil {
		return nil, err
	}
	upd := r.ent.TrafficPolicy.UpdateOneID(id).SetUpdatedAt(time.Now())
	if in.Name != nil {
		upd = upd.SetName(*in.Name)
	}
	if in.Priority != nil {
		upd = upd.SetPriority(*in.Priority)
	}
	if in.Match != nil {
		matchRaw, err := marshalMatch(in.Match)
		if err != nil {
			return nil, err
		}
		upd = upd.SetMatch(matchRaw)
	}
	if in.TargetVersionID != nil {
		if *in.TargetVersionID == uuid.Nil {
			upd = upd.ClearTargetVersionID()
		} else {
			upd = upd.SetTargetVersionID(*in.TargetVersionID)
		}
	}
	if in.Weight != nil {
		upd = upd.SetWeight(*in.Weight)
	}
	if in.Sticky != nil {
		stickyRaw, err := marshalSticky(in.Sticky)
		if err != nil {
			return nil, err
		}
		if stickyRaw == nil {
			upd = upd.ClearSticky()
		} else {
			upd = upd.SetSticky(stickyRaw)
		}
	}
	if in.Status != nil {
		upd = upd.SetStatus(string(*in.Status))
	}
	e, err := upd.Save(ctx)
	if err != nil {
		if pkg.IsUniqueViolation(err) {
			return nil, pkg.ErrConflict("该服务下已存在同名策略")
		}
		return nil, fmt.Errorf("update traffic policy: %w", err)
	}
	return fromEnt(e)
}

// SetStatus 便捷状态变更。
func (r *Repository) SetStatus(ctx context.Context, id uuid.UUID, s Status) (*Policy, error) {
	return r.Update(ctx, id, UpdateInput{Status: &s})
}

// Delete 删除策略。
func (r *Repository) Delete(ctx context.Context, id uuid.UUID) error {
	err := r.ent.TrafficPolicy.DeleteOneID(id).Exec(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return pkg.ErrNotFound("策略不存在")
		}
		return fmt.Errorf("delete traffic policy: %w", err)
	}
	return nil
}

func collectPolicies(es []*ent.TrafficPolicy) ([]*Policy, error) {
	out := make([]*Policy, 0, len(es))
	for _, e := range es {
		p, err := fromEnt(e)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, nil
}
