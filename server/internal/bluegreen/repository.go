package bluegreen

import (
	"context"
	"fmt"
	"time"

	"entgo.io/ent/dialect/sql"
	"github.com/NeoPlayful/maple-gateway/server/ent"
	entbg "github.com/NeoPlayful/maple-gateway/server/ent/bluegreendeployment"
	"github.com/NeoPlayful/maple-gateway/server/pkg"
	"github.com/google/uuid"
)

// Repository 是 Blue/Green 数据访问层。
// 普通 CRUD 与 Transition 均走 Ent；Transition 以事务内乐观条件更新防并发。
type Repository struct {
	ent *ent.Client
}

// NewRepository 构造。
func NewRepository(client *ent.Client) *Repository {
	return &Repository{ent: client}
}

func toModel(e *ent.BluegreenDeployment) *BGDeployment {
	return &BGDeployment{
		ID:               e.ID,
		DeploymentID:     e.DeploymentID,
		BlueVersionID:    e.BlueVersionID,
		GreenVersionID:   e.GreenVersionID,
		ActiveVersionID:  e.ActiveVersionID,
		PreviousActiveID: e.PreviousActiveID,
		CreatedAt:        e.CreatedAt,
		UpdatedAt:        e.UpdatedAt,
	}
}

// Create 登记 BG 配置（deployment 级唯一）。
func (r *Repository) Create(ctx context.Context, in NewBG) (*BGDeployment, error) {
	if in.BlueVersionID == in.GreenVersionID {
		return nil, pkg.ErrValidation("blue 与 green 版本不能相同")
	}
	active := in.InitialActive
	if active == uuid.Nil {
		active = in.BlueVersionID
	}
	now := time.Now()
	e, err := r.ent.BluegreenDeployment.Create().
		SetDeploymentID(in.DeploymentID).
		SetBlueVersionID(in.BlueVersionID).
		SetGreenVersionID(in.GreenVersionID).
		SetActiveVersionID(active).
		SetCreatedAt(now).
		SetUpdatedAt(now).
		Save(ctx)
	if err != nil {
		if pkg.IsUniqueViolation(err) {
			return nil, pkg.ErrConflict("该部署已配置 Blue/Green")
		}
		if pkg.IsForeignKeyViolation(err) {
			return nil, pkg.ErrValidation("deployment_id 或版本 ID 不存在")
		}
		return nil, fmt.Errorf("insert bluegreen: %w", err)
	}
	return toModel(e), nil
}

// Get 查询。
func (r *Repository) Get(ctx context.Context, id uuid.UUID) (*BGDeployment, error) {
	e, err := r.ent.BluegreenDeployment.Get(ctx, id)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, pkg.ErrNotFound("Blue/Green 配置不存在")
		}
		return nil, fmt.Errorf("get bluegreen: %w", err)
	}
	return toModel(e), nil
}

// List 分页列出（deploymentID 可选）。
func (r *Repository) List(ctx context.Context, deploymentID *uuid.UUID, limit, offset int) ([]*BGDeployment, int, error) {
	q := r.ent.BluegreenDeployment.Query()
	if deploymentID != nil {
		q = q.Where(entbg.DeploymentID(*deploymentID))
	}
	total, err := q.Count(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("count bluegreen: %w", err)
	}
	es, err := q.
		Order(entbg.ByCreatedAt(sql.OrderDesc())).
		Limit(limit).
		Offset(offset).
		All(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("list bluegreen: %w", err)
	}
	out := make([]*BGDeployment, 0, len(es))
	for _, e := range es {
		out = append(out, toModel(e))
	}
	return out, total, nil
}

// Delete 删除 BG 配置（不改变版本状态）。
func (r *Repository) Delete(ctx context.Context, id uuid.UUID) error {
	err := r.ent.BluegreenDeployment.DeleteOneID(id).Exec(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return pkg.ErrNotFound("Blue/Green 配置不存在")
		}
		return fmt.Errorf("delete bluegreen: %w", err)
	}
	return nil
}

// Transition 事务内执行角色翻转动作。
func (r *Repository) Transition(ctx context.Context, id uuid.UUID,
	fn func(ctx context.Context, tc *ent.Client, bg *BGDeployment) error) (*BGDeployment, error) {
	tx, err := r.ent.Tx(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	tc := tx.Client()

	bg, err := toBG(tc.BluegreenDeployment.Get(ctx, id))
	if err != nil {
		return nil, err
	}
	if err := fn(ctx, tc, bg); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return r.Get(ctx, id)
}

func toBG(e *ent.BluegreenDeployment, err error) (*BGDeployment, error) {
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, pkg.ErrNotFound("Blue/Green 配置不存在")
		}
		return nil, err
	}
	return toModel(e), nil
}

// ---------- 事务内 helper ----------

// flipActive 把 bg.active 翻转为 target：active 版本 weight 100 + 状态 active；
// 另一版本 weight 0 + standby。recordPrev 为 true 时把翻转前的 active 记为 previous（供回滚）。
// 乐观条件：WHERE active_version_id=bg.ActiveVersionID，防止基于过期状态并发翻转。
func flipActive(ctx context.Context, c *ent.Client, bg *BGDeployment, target uuid.UUID, recordPrev bool) error {
	if target != bg.BlueVersionID && target != bg.GreenVersionID {
		return pkg.ErrValidation("target 必须是 blue 或 green 版本")
	}
	other := bg.BlueVersionID
	if target == bg.BlueVersionID {
		other = bg.GreenVersionID
	}
	if err := setVersionRole(ctx, c, target, "active", 100); err != nil {
		return err
	}
	if err := setVersionRole(ctx, c, other, "standby", 0); err != nil {
		return err
	}
	upd := c.BluegreenDeployment.Update().
		Where(
			entbg.ID(bg.ID),
			entbg.ActiveVersionID(bg.ActiveVersionID),
		).
		SetActiveVersionID(target).
		SetUpdatedAt(time.Now())
	if recordPrev {
		upd = upd.SetPreviousActiveID(bg.ActiveVersionID)
	} else {
		// initial 激活：previous 无意义，置 NULL。
		upd = upd.ClearPreviousActiveID()
	}
	n, err := upd.Save(ctx)
	if err != nil {
		return fmt.Errorf("update bluegreen active: %w", err)
	}
	if n == 0 {
		return pkg.ErrConflict("Blue/Green active 已被并发变更，请重试")
	}
	return nil
}

func setVersionRole(ctx context.Context, c *ent.Client, id uuid.UUID, status string, weight int) error {
	if err := c.DeploymentVersion.UpdateOneID(id).
		SetStatus(status).
		SetWeight(weight).
		SetUpdatedAt(time.Now()).
		Exec(ctx); err != nil {
		return fmt.Errorf("set version role: %w", err)
	}
	return nil
}

// insertEvent 写切换历史。
func insertEvent(ctx context.Context, c *ent.Client, bgID uuid.UUID, action string,
	from, to uuid.UUID, detail string) error {
	if err := c.BluegreenEvent.Create().
		SetBgID(bgID).
		SetAction(action).
		SetFromActive(from).
		SetToActive(to).
		SetDetail(detail).
		SetCreatedAt(time.Now()).
		Exec(ctx); err != nil {
		return fmt.Errorf("insert bluegreen event: %w", err)
	}
	return nil
}
