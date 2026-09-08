package bluegreen

import (
	"context"
	"fmt"

	"github.com/NeoPlayful/maple-gateway/server/ent"
	"github.com/NeoPlayful/maple-gateway/server/pkg"
	"github.com/google/uuid"
)

// Service 编排 Blue/Green 动作。
type Service struct {
	repo *Repository
}

// NewService 构造。
func NewService(repo *Repository) *Service {
	return &Service{repo: repo}
}

// List 列出（deploymentID 可选）。
func (s *Service) List(ctx context.Context, deploymentID *uuid.UUID, limit, offset int) ([]*BGDeployment, int, error) {
	return s.repo.List(ctx, deploymentID, limit, offset)
}

// Get 查询单个。
func (s *Service) Get(ctx context.Context, id uuid.UUID) (*BGDeployment, error) {
	return s.repo.Get(ctx, id)
}

// Delete 删除配置（不改版本状态）。
func (s *Service) Delete(ctx context.Context, id uuid.UUID) error {
	return s.repo.Delete(ctx, id)
}

// Create 登记 BG 配置并把 initial_active 置为 active(100)，另一版本 standby(0)。
func (s *Service) Create(ctx context.Context, in NewBG) (*BGDeployment, error) {
	if in.BlueVersionID == in.GreenVersionID {
		return nil, pkg.ErrValidation("blue 与 green 版本不能相同")
	}
	// 先落库拿到 id，再在同一动作里翻转角色。为保持单事务，直接先 Create 再套翻转会有两次提交。
	// 简化：登记后由 handler 一次性调用 ApplyInitial。这里仅落库。
	return s.repo.Create(ctx, in)
}

// ApplyInitial 使 initial_active 立即生效（双版本归属校验 + 翻转）。
func (s *Service) ApplyInitial(ctx context.Context, id uuid.UUID) (*BGDeployment, error) {
	return s.repo.Transition(ctx, id, func(ctx context.Context, c *ent.Client, bg *BGDeployment) error {
		// 校验 blue/green 归属同一部署。
		blueDep, err := versionDeployment(ctx, c, bg.BlueVersionID)
		if err != nil {
			return err
		}
		greenDep, err := versionDeployment(ctx, c, bg.GreenVersionID)
		if err != nil {
			return err
		}
		if blueDep != bg.DeploymentID || greenDep != bg.DeploymentID {
			return pkg.ErrValidation("blue/green 版本必须属于该部署")
		}
		if err := flipActive(ctx, c, bg, bg.ActiveVersionID, false); err != nil {
			return err
		}
		return insertEvent(ctx, c, bg.ID, "switch", uuid.Nil, bg.ActiveVersionID,
			"initial activate: "+bg.ActiveVersionID.String())
	})
}

// Switch 切换 active 到目标版本。
func (s *Service) Switch(ctx context.Context, id uuid.UUID, target uuid.UUID) (*BGDeployment, error) {
	return s.repo.Transition(ctx, id, func(ctx context.Context, c *ent.Client, bg *BGDeployment) error {
		if target == bg.ActiveVersionID {
			return pkg.ErrValidation("target 已是当前 active 版本")
		}
		if err := flipActive(ctx, c, bg, target, true); err != nil {
			return err
		}
		return insertEvent(ctx, c, bg.ID, "switch", bg.ActiveVersionID, target,
			fmt.Sprintf("switch active to %s", target))
	})
}

// Rollback 切回上一 active。
func (s *Service) Rollback(ctx context.Context, id uuid.UUID) (*BGDeployment, error) {
	return s.repo.Transition(ctx, id, func(ctx context.Context, c *ent.Client, bg *BGDeployment) error {
		if bg.PreviousActiveID == nil {
			return pkg.ErrConflict("无上一 active 可回滚")
		}
		prev := *bg.PreviousActiveID
		if err := flipActive(ctx, c, bg, prev, true); err != nil {
			return err
		}
		return insertEvent(ctx, c, bg.ID, "rollback", bg.ActiveVersionID, prev,
			"rollback to previous active "+prev.String())
	})
}

// versionDeployment 查询版本所属 deployment。
func versionDeployment(ctx context.Context, c *ent.Client, id uuid.UUID) (uuid.UUID, error) {
	v, err := c.DeploymentVersion.Get(ctx, id)
	if err != nil {
		return uuid.Nil, pkg.ErrValidation("版本不存在: " + id.String())
	}
	return v.DeploymentID, nil
}
