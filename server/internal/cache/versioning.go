package cache

import (
	"context"
	"fmt"

	"github.com/NeoPlayful/maple-gateway/server/internal/deployment"
	"github.com/NeoPlayful/maple-gateway/server/internal/traffic"
	"github.com/google/uuid"
)

// versionSource 默认 VersionSource 实现：从 deployment + traffic repository 加载。
type versionSource struct {
	deploy *deployment.Repository
	traffic *traffic.Repository
}

// NewVersionSource 构造默认版本/策略加载源。
// 任一 repo 为 nil 时等价于无版本分流（Phase 1 行为）。
func NewVersionSource(dep *deployment.Repository, tr *traffic.Repository) VersionSource {
	return &versionSource{deploy: dep, traffic: tr}
}

// LoadVersioning 实现 VersionSource。
func (s *versionSource) LoadVersioning(ctx context.Context) (map[uuid.UUID][]VersionGroup,
	map[uuid.UUID][]traffic.Policy, error) {

	groups := map[uuid.UUID][]VersionGroup{}
	if s.deploy != nil {
		rows, err := s.deploy.AllVersionGroups(ctx)
		if err != nil {
			return nil, nil, fmt.Errorf("load version groups: %w", err)
		}
		for _, g := range rows {
			groups[g.ServiceID] = append(groups[g.ServiceID], VersionGroup{
				DeploymentID: g.DeploymentID,
				VersionID:    g.VersionID,
				Version:      g.Version,
				Weight:       g.Weight,
				Status:       string(g.Status),
			})
		}
	}

	policies := map[uuid.UUID][]traffic.Policy{}
	if s.traffic != nil {
		rows, err := s.traffic.AllGroupedByService(ctx)
		if err != nil {
			return nil, nil, fmt.Errorf("load traffic policies: %w", err)
		}
		for sid, ps := range rows {
			vals := make([]traffic.Policy, 0, len(ps))
			for _, p := range ps {
				vals = append(vals, *p)
			}
			policies[sid] = vals
		}
	}
	return groups, policies, nil
}
