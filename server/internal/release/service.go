package release

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/NeoPlayful/maple-gateway/server/ent"
	"github.com/NeoPlayful/maple-gateway/server/pkg"
	"github.com/google/uuid"
)

// Service 编排发布动作；每个动作在事务内原子改版本权重/状态与发布记录。
type Service struct {
	repo *Repository
}

// NewService 构造。
func NewService(repo *Repository) *Service {
	return &Service{repo: repo}
}

// List 列出（strategy / serviceID / phase 可选）。
func (s *Service) List(ctx context.Context, strategy *Strategy, serviceID *uuid.UUID,
	phase Phase, limit, offset int) ([]*Release, int, error) {
	return s.repo.List(ctx, strategy, serviceID, phase, limit, offset)
}

// Get 返回发布。
func (s *Service) Get(ctx context.Context, id uuid.UUID) (*Release, error) {
	return s.repo.Get(ctx, id)
}

// Events 返回某发布的事件流水（新→旧）。
func (s *Service) Events(ctx context.Context, id uuid.UUID) ([]*Event, error) {
	return s.repo.Events(ctx, id)
}

// Create 按 strategy 分发改登记。canary 创建后不切流量；bluegreen 创建即初始激活。
func (s *Service) Create(ctx context.Context, in NewRelease) (*Release, error) {
	if !ValidStrategy(in.Strategy) {
		return nil, pkg.ErrValidation("strategy 必须是 canary 或 bluegreen")
	}
	st, err := strategyFor(in.Strategy)
	if err != nil {
		return nil, err
	}
	var created *Release
	err = s.repo.WithTx(ctx, func(tc *ent.Client) error {
		plan, err := st.validateCreate(ctx, tc, in)
		if err != nil {
			return err
		}
		rel, err := s.repo.CreateTx(ctx, tc, plan)
		if err != nil {
			return err
		}
		// 蓝绿创建即翻转角色；canary 创建时 allocations 为空。
		if len(plan.allocations) > 0 {
			if err := applyAllocation(ctx, tc, plan.allocations); err != nil {
				return err
			}
		}
		created = rel
		return nil
	})
	if err != nil {
		return nil, err
	}
	return created, nil
}

// Update 改配置（不触发状态机）。
func (s *Service) Update(ctx context.Context, id uuid.UUID, in UpdateRelease) (*Release, error) {
	return s.repo.Update(ctx, id, in)
}

// Delete 删除记录；运行中发布禁止删除。
func (s *Service) Delete(ctx context.Context, id uuid.UUID) error {
	rel, err := s.repo.Get(ctx, id)
	if err != nil {
		return err
	}
	switch rel.Phase {
	case PhaseRunning, PhasePaused, PhaseActive, PhaseCreated:
		return pkg.ErrConflict("活跃中的发布不能删除，请先促进/回滚/停止")
	}
	return s.repo.Delete(ctx, id)
}

// Start canary 开始发布（bluegreen 无此动作）。
func (s *Service) Start(ctx context.Context, id uuid.UUID) (*Release, error) {
	return s.act(ctx, id, ActionStart, nil)
}

// Pause canary 暂停。
func (s *Service) Pause(ctx context.Context, id uuid.UUID) (*Release, error) {
	return s.act(ctx, id, ActionPause, nil)
}

// Resume canary 恢复。
func (s *Service) Resume(ctx context.Context, id uuid.UUID) (*Release, error) {
	return s.act(ctx, id, ActionResume, nil)
}

// Promote canary 晋升。
func (s *Service) Promote(ctx context.Context, id uuid.UUID) (*Release, error) {
	return s.act(ctx, id, ActionPromote, nil)
}

// Rollback 回滚（canary 排空 / bluegreen 切回上一 active）。
func (s *Service) Rollback(ctx context.Context, id uuid.UUID) (*Release, error) {
	return s.act(ctx, id, ActionRollback, nil)
}

// SetWeight 调整 canary 权重。
func (s *Service) SetWeight(ctx context.Context, id uuid.UUID, w int) (*Release, error) {
	return s.act(ctx, id, ActionWeight, w)
}

// Switch 切换 bluegreen active。
func (s *Service) Switch(ctx context.Context, id uuid.UUID, target uuid.UUID) (*Release, error) {
	return s.act(ctx, id, ActionSwitch, target)
}

// act 是统一动作入口：读发布 → 按 strategy 算 allocation 与状态迁移 → 同一事务内落库。
// 这是"记录模型 + 效果写入 + 不变量"统一的核心；差异全部收敛在 strategy.action 里。
func (s *Service) act(ctx context.Context, id uuid.UUID, action Action, arg any) (*Release, error) {
	// 先读一次拿 strategy（事务外轻读，动作内仍以事务读到的 rel 为准）。
	head, err := s.repo.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	st, err := strategyFor(head.Strategy)
	if err != nil {
		return nil, err
	}
	return s.repo.Transition(ctx, id, func(ctx context.Context, tc *ent.Client, rel *Release) error {
		res, err := st.action(ctx, tc, rel, string(action), arg)
		if err != nil {
			return err
		}
		if len(res.allocations) > 0 {
			if err := applyAllocation(ctx, tc, res.allocations); err != nil {
				return err
			}
		}
		// bluegreen 的 switch/rollback 需记录 previous（翻转前的 primary）。
		var previous *uuid.UUID
		if rel.Strategy == StrategyBlueGreen {
			switch action {
			case ActionSwitch, ActionRollback:
				old := rel.PrimaryVersionID
				previous = &old
			}
		}
		if _, err := updateState(ctx, tc, rel, res, previous); err != nil {
			return err
		}
		from, to := weightsForEvent(rel, res)
		fromVer, toVer := versionsForEvent(rel, res, action)
		return insertEvent(ctx, tc, rel.ID, action, from, to, fromVer, toVer, eventDetail(action, rel, res, arg))
	})
}

// weightsForEvent 计算事件的 from/to 权重（canary 用 secondary=canary 权重）。
func weightsForEvent(rel *Release, res *actionResult) (*int, *int) {
	if rel.Strategy == StrategyCanary {
		return intPtr(rel.SecondaryWeight), intPtr(res.SecondaryWeight)
	}
	return nil, nil
}

// versionsForEvent 计算事件的 from/to 版本（bluegreen 用 primary=active）。
func versionsForEvent(rel *Release, res *actionResult, action Action) (*uuid.UUID, *uuid.UUID) {
	if rel.Strategy != StrategyBlueGreen {
		return nil, nil
	}
	switch action {
	case ActionSwitch, ActionRollback:
		from := rel.PrimaryVersionID
		var newActive *uuid.UUID
		for _, a := range res.allocations {
			if a.Status == "active" {
				v := a.VersionID
				newActive = &v
				break
			}
		}
		return &from, newActive
	}
	return nil, nil
}

func eventDetail(action Action, rel *Release, res *actionResult, arg any) string {
	switch action {
	case ActionStart:
		return fmt.Sprintf("canary started at %d%%", res.SecondaryWeight)
	case ActionPause:
		return fmt.Sprintf("paused at %d%%", rel.SecondaryWeight)
	case ActionResume:
		return fmt.Sprintf("resumed at %d%%", rel.SecondaryWeight)
	case ActionWeight:
		return fmt.Sprintf("canary weight %d%% → %d%%", rel.SecondaryWeight, res.SecondaryWeight)
	case ActionPromote:
		return "promote: canary promoted to stable 100%, old stable draining"
	case ActionRollback:
		if rel.Strategy == StrategyBlueGreen {
			return "rollback to previous active"
		}
		return "rollback: canary drained to 0%, stable weight restored to 100%"
	case ActionSwitch:
		return fmt.Sprintf("switch active, target=%v", arg)
	}
	return string(action)
}

// ---------- Repository 的事务辅助（供 service 使用） ----------

// WithTx 在单事务内执行 fn（供创建等需要多步原子的场景）。
func (r *Repository) WithTx(ctx context.Context, fn func(tc *ent.Client) error) error {
	tx, err := r.ent.Tx(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err := fn(tx.Client()); err != nil {
		return err
	}
	return tx.Commit()
}

// marshalConfig 序列化 canary 配置（repository.Update 用）。
func marshalConfig(c CanaryConfig) (json.RawMessage, error) {
	return json.Marshal(c)
}
