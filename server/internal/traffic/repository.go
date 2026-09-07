package traffic

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/NeoPlayful/maple-gateway/server/pkg"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
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

// Repository 是流量策略数据访问层。
type Repository struct {
	pool *pgxpool.Pool
}

// NewRepository 构造。
func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

const cols = `id, service_id, name, priority, match, target_version_id, weight, sticky, status, created_at, updated_at`

func scanPolicy(row pgx.Row) (*Policy, error) {
	var p Policy
	var matchRaw, stickyRaw json.RawMessage
	err := row.Scan(&p.ID, &p.ServiceID, &p.Name, &p.Priority, &matchRaw, &p.TargetVersionID,
		&p.Weight, &stickyRaw, &p.Status, &p.CreatedAt, &p.UpdatedAt)
	if err != nil {
		return nil, err
	}
	m, err := NewMatch(matchRaw)
	if err != nil {
		return nil, fmt.Errorf("parse policy %s match: %w", p.ID, err)
	}
	p.Match = m
	s, err := NewSticky(stickyRaw)
	if err != nil {
		return nil, fmt.Errorf("parse policy %s sticky: %w", p.ID, err)
	}
	p.Sticky = s
	return &p, nil
}

func marshalMatch(m *Match) ([]byte, error) {
	if m == nil {
		return json.Marshal(Match{})
	}
	return json.Marshal(m)
}

func marshalSticky(s *Sticky) ([]byte, error) {
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
	row := r.pool.QueryRow(ctx, `
		INSERT INTO traffic_policies(service_id, name, priority, match, target_version_id, weight, sticky, status)
		VALUES($1, $2, $3, $4, $5, $6, $7, 'enabled')
		RETURNING `+cols,
		in.ServiceID, in.Name, priority, matchRaw, in.TargetVersionID, in.Weight, stickyRaw)
	p, err := scanPolicy(row)
	if err != nil {
		if pkg.IsUniqueViolation(err) {
			return nil, pkg.ErrConflict("该服务下已存在同名策略")
		}
		if pkg.IsForeignKeyViolation(err) {
			return nil, pkg.ErrValidation("service_id 或 target_version_id 不存在")
		}
		return nil, fmt.Errorf("insert traffic policy: %w", err)
	}
	return p, nil
}

// ListByService 列出某服务的策略（按 priority 升序）。
func (r *Repository) ListByService(ctx context.Context, serviceID uuid.UUID) ([]*Policy, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT `+cols+` FROM traffic_policies
		WHERE service_id=$1 ORDER BY priority, created_at`, serviceID)
	if err != nil {
		return nil, fmt.Errorf("list traffic policies by service: %w", err)
	}
	defer rows.Close()
	return collectPolicies(rows)
}

// ListAll 分页列出全部策略。
func (r *Repository) ListAll(ctx context.Context, limit, offset int) ([]*Policy, int, error) {
	var total int
	if err := r.pool.QueryRow(ctx, `SELECT count(*) FROM traffic_policies`).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count traffic policies: %w", err)
	}
	rows, err := r.pool.Query(ctx, `
		SELECT `+cols+` FROM traffic_policies
		ORDER BY created_at DESC LIMIT $1 OFFSET $2`, limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("list traffic policies: %w", err)
	}
	defer rows.Close()
	out, err := collectPolicies(rows)
	return out, total, err
}

// AllGroupedByService 返回全部策略按 service_id 分组（路由表构建用）。
func (r *Repository) AllGroupedByService(ctx context.Context) (map[uuid.UUID][]*Policy, error) {
	rows, err := r.pool.Query(ctx, `SELECT `+cols+` FROM traffic_policies ORDER BY service_id, priority`)
	if err != nil {
		return nil, fmt.Errorf("all traffic policies grouped: %w", err)
	}
	defer rows.Close()
	out := map[uuid.UUID][]*Policy{}
	for rows.Next() {
		p, err := scanPolicy(rows)
		if err != nil {
			return nil, err
		}
		out[p.ServiceID] = append(out[p.ServiceID], p)
	}
	return out, rows.Err()
}

// GetByID 查询策略。
func (r *Repository) GetByID(ctx context.Context, id uuid.UUID) (*Policy, error) {
	p, err := scanPolicy(r.pool.QueryRow(ctx, `SELECT `+cols+` FROM traffic_policies WHERE id=$1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, pkg.ErrNotFound("策略不存在")
	}
	if err != nil {
		return nil, fmt.Errorf("get traffic policy: %w", err)
	}
	return p, nil
}

// Update 应用非空更新。
func (r *Repository) Update(ctx context.Context, id uuid.UUID, in UpdateInput) (*Policy, error) {
	p, err := r.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if in.Name != nil {
		p.Name = *in.Name
	}
	if in.Priority != nil {
		p.Priority = *in.Priority
	}
	if in.Match != nil {
		p.Match = *in.Match
	}
	if in.TargetVersionID != nil {
		if *in.TargetVersionID == uuid.Nil {
			p.TargetVersionID = nil
		} else {
			p.TargetVersionID = in.TargetVersionID
		}
	}
	if in.Weight != nil {
		p.Weight = *in.Weight
	}
	if in.Sticky != nil {
		p.Sticky = in.Sticky
	}
	if in.Status != nil {
		p.Status = *in.Status
	}
	matchRaw, _ := marshalMatch(&p.Match)
	stickyRaw, _ := marshalSticky(p.Sticky)
	upd, err := scanPolicy(r.pool.QueryRow(ctx, `
		UPDATE traffic_policies SET name=$2, priority=$3, match=$4, target_version_id=$5,
			weight=$6, sticky=$7, status=$8, updated_at=now()
		WHERE id=$1 RETURNING `+cols,
		id, p.Name, p.Priority, matchRaw, p.TargetVersionID, p.Weight, stickyRaw, p.Status))
	if err != nil {
		if pkg.IsUniqueViolation(err) {
			return nil, pkg.ErrConflict("该服务下已存在同名策略")
		}
		return nil, fmt.Errorf("update traffic policy: %w", err)
	}
	return upd, nil
}

// SetStatus 便捷状态变更。
func (r *Repository) SetStatus(ctx context.Context, id uuid.UUID, s Status) (*Policy, error) {
	return r.Update(ctx, id, UpdateInput{Status: &s})
}

// Delete 删除策略。
func (r *Repository) Delete(ctx context.Context, id uuid.UUID) error {
	tag, err := r.pool.Exec(ctx, `DELETE FROM traffic_policies WHERE id=$1`, id)
	if err != nil {
		return fmt.Errorf("delete traffic policy: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return pkg.ErrNotFound("策略不存在")
	}
	return nil
}

func collectPolicies(rows pgx.Rows) ([]*Policy, error) {
	out := []*Policy{}
	for rows.Next() {
		p, err := scanPolicy(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}
