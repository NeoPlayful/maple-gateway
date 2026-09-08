package canary

import (
	"context"
	"errors"
	"fmt"

	"github.com/NeoPlayful/maple-gateway/server/ent"
	"github.com/NeoPlayful/maple-gateway/server/pkg"
	"github.com/google/uuid"
)

// Service 编排 Canary 状态机动作；每个动作在事务内原子改版本权重与状态。
type Service struct {
	repo *Repository
}

// NewService 构造。
func NewService(repo *Repository) *Service {
	return &Service{repo: repo}
}

// List 列出发布（serviceID 可选）。
func (s *Service) List(ctx context.Context, serviceID *uuid.UUID, limit, offset int) ([]*Release, int, error) {
	return s.repo.List(ctx, serviceID, Phase(""), limit, offset)
}

// Create 创建发布记录（仅登记，不切流量）。
func (s *Service) Create(ctx context.Context, in NewRelease) (*Release, error) {
	if in.StableVersionID == in.CanaryVersionID {
		return nil, pkg.ErrValidation("stable 与 canary 版本不能相同")
	}
	if in.InitialWeight == 0 {
		in.InitialWeight = 10
	}
	if in.TargetWeight == 0 {
		in.TargetWeight = 100
	}
	if in.StepWeight == 0 {
		in.StepWeight = 10
	}
	return s.repo.Create(ctx, in)
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
	if rel.Phase == PhaseRunning || rel.Phase == PhasePaused {
		return pkg.ErrConflict("运行中的发布不能删除，请先 promote/rollback")
	}
	return s.repo.Delete(ctx, id)
}

// Get 返回发布。
func (s *Service) Get(ctx context.Context, id uuid.UUID) (*Release, error) {
	return s.repo.Get(ctx, id)
}

// Start 开始发布：校验版本同属一部署且不冲突后，把 canary 权重设为 initial(默认10)。
func (s *Service) Start(ctx context.Context, id uuid.UUID) (*Release, error) {
	return s.repo.Transition(ctx, id, func(ctx context.Context, c *ent.Client, rel *Release) error {
		switch rel.Phase {
		case PhaseCreated, PhasePaused, PhaseRolledBack:
		default:
			return pkg.ErrConflict("发布当前状态不可 start（phase=" + string(rel.Phase) + ")")
		}
		if err := s.validatePair(ctx, c, rel); err != nil {
			return err
		}
		cw := rel.CanaryWeight
		if cw == 0 {
			cw = 10
		}
		if err := applyWeights(ctx, c, rel, cw); err != nil {
			return err
		}
		if _, err := updateReleaseState(ctx, c, rel, PhaseRunning, cw, true, false); err != nil {
			return err
		}
		return insertEvent(ctx, c, rel.ID, ActionStart, intPtr(0), intPtr(cw),
			fmt.Sprintf("canary started at %d%%", cw))
	})
}

// Pause 暂停（冻结权重，不切流量）。
func (s *Service) Pause(ctx context.Context, id uuid.UUID) (*Release, error) {
	return s.repo.Transition(ctx, id, func(ctx context.Context, c *ent.Client, rel *Release) error {
		if rel.Phase != PhaseRunning {
			return pkg.ErrConflict("仅 running 状态可 pause")
		}
		if _, err := updateReleaseState(ctx, c, rel, PhasePaused, rel.CanaryWeight, false, false); err != nil {
			return err
		}
		return insertEvent(ctx, c, rel.ID, ActionPause, nil, nil,
			fmt.Sprintf("paused at %d%%", rel.CanaryWeight))
	})
}

// Resume 恢复。
func (s *Service) Resume(ctx context.Context, id uuid.UUID) (*Release, error) {
	return s.repo.Transition(ctx, id, func(ctx context.Context, c *ent.Client, rel *Release) error {
		if rel.Phase != PhasePaused {
			return pkg.ErrConflict("仅 paused 状态可 resume")
		}
		if _, err := updateReleaseState(ctx, c, rel, PhaseRunning, rel.CanaryWeight, false, false); err != nil {
			return err
		}
		return insertEvent(ctx, c, rel.ID, ActionResume, nil, nil,
			fmt.Sprintf("resumed at %d%%", rel.CanaryWeight))
	})
}

// SetWeight 调整 canary 权重（0-100）。stable 自动 = 100 - w。
func (s *Service) SetWeight(ctx context.Context, id uuid.UUID, w int) (*Release, error) {
	if w < 0 || w > 100 {
		return nil, pkg.ErrValidation("weight 须在 0-100")
	}
	return s.repo.Transition(ctx, id, func(ctx context.Context, c *ent.Client, rel *Release) error {
		if rel.Phase != PhaseRunning && rel.Phase != PhasePaused {
			return pkg.ErrConflict("仅 running/paused 状态可调权重")
		}
		from := rel.CanaryWeight
		if err := applyWeights(ctx, c, rel, w); err != nil {
			return err
		}
		if _, err := updateReleaseState(ctx, c, rel, rel.Phase, w, false, false); err != nil {
			return err
		}
		return insertEvent(ctx, c, rel.ID, ActionWeight, intPtr(from), intPtr(w),
			fmt.Sprintf("canary weight %d%% → %d%%", from, w))
	})
}

// Promote 晋升：canary → stable（weight 100），原 stable → draining（weight 0）。
func (s *Service) Promote(ctx context.Context, id uuid.UUID) (*Release, error) {
	return s.repo.Transition(ctx, id, func(ctx context.Context, c *ent.Client, rel *Release) error {
		if rel.Phase != PhaseRunning && rel.Phase != PhasePaused {
			return pkg.ErrConflict("仅 running/paused 状态可 promote")
		}
		if err := setVersionStatus(ctx, c, rel.CanaryVersionID, "stable"); err != nil {
			return err
		}
		if err := setVersionWeight(ctx, c, rel.CanaryVersionID, 100); err != nil {
			return err
		}
		if err := setVersionStatus(ctx, c, rel.StableVersionID, "draining"); err != nil {
			return err
		}
		if err := setVersionWeight(ctx, c, rel.StableVersionID, 0); err != nil {
			return err
		}
		if _, err := updateReleaseState(ctx, c, rel, PhaseCompleted, 100, false, true); err != nil {
			return err
		}
		return insertEvent(ctx, c, rel.ID, ActionPromote, intPtr(rel.CanaryWeight), intPtr(100),
			"promote: canary promoted to stable 100%, old stable draining")
	})
}

// Rollback 回滚：canary 排空（weight 0 → standby），原 stable 恢复 100 并回到 stable。
func (s *Service) Rollback(ctx context.Context, id uuid.UUID) (*Release, error) {
	return s.repo.Transition(ctx, id, func(ctx context.Context, c *ent.Client, rel *Release) error {
		switch rel.Phase {
		case PhaseRunning, PhasePaused, PhaseCreated:
		default:
			return pkg.ErrConflict("当前状态不可 rollback（phase=" + string(rel.Phase) + ")")
		}
		if err := setVersionWeight(ctx, c, rel.CanaryVersionID, 0); err != nil {
			return err
		}
		if err := setVersionStatus(ctx, c, rel.CanaryVersionID, "standby"); err != nil {
			return err
		}
		if err := setVersionStatus(ctx, c, rel.StableVersionID, "stable"); err != nil {
			return err
		}
		if err := setVersionWeight(ctx, c, rel.StableVersionID, 100); err != nil {
			return err
		}
		if _, err := updateReleaseState(ctx, c, rel, PhaseRolledBack, 0, false, true); err != nil {
			return err
		}
		return insertEvent(ctx, c, rel.ID, ActionRollback, intPtr(rel.CanaryWeight), intPtr(0),
			"rollback: canary drained to 0%, stable weight restored to 100%")
	})
}

// validatePair 校验 stable/canary 版本属于同一部署，且无其它活跃(same service)发布。
func (s *Service) validatePair(ctx context.Context, c *ent.Client, rel *Release) error {
	sw, _, _, sDep, err := versionRef(ctx, c, rel.StableVersionID)
	if err != nil {
		return err
	}
	_, _, _, cDep, err := versionRef(ctx, c, rel.CanaryVersionID)
	if err != nil {
		return err
	}
	if sDep != cDep {
		return pkg.ErrValidation("stable 与 canary 版本必须属于同一部署")
	}
	if sw != 100 {
		return pkg.ErrValidation("stable 版本当前权重非 100%（存在其它分流），无法作为基线")
	}
	return nil
}

// applyWeights 设置 canary=w、stable=100-w 两个版本的 weight。
func applyWeights(ctx context.Context, c *ent.Client, rel *Release, w int) error {
	if err := setVersionWeight(ctx, c, rel.CanaryVersionID, w); err != nil {
		return err
	}
	return setVersionWeight(ctx, c, rel.StableVersionID, 100-w)
}

func intPtr(i int) *int { return &i }

var _ = errors.Is // 保留 errors 导入（扩展用）
