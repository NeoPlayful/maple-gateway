package dashboard

import (
	"context"
	"fmt"

	"entgo.io/ent/dialect/sql"
	"github.com/NeoPlayful/maple-gateway/server/ent"
	entcanary "github.com/NeoPlayful/maple-gateway/server/ent/canaryrelease"
	entinstance "github.com/NeoPlayful/maple-gateway/server/ent/instance"
	entnode "github.com/NeoPlayful/maple-gateway/server/ent/node"
	entservice "github.com/NeoPlayful/maple-gateway/server/ent/service"
	"github.com/google/uuid"
)

// Repository 提供 Dashboard 只读聚合查询（基于 Ent）。
type Repository struct {
	ent *ent.Client
}

// NewRepository 构造。
func NewRepository(client *ent.Client) *Repository {
	return &Repository{ent: client}
}

// Overview 聚合 Dashboard 总览所需数据：计数、实例/节点分布、进行中的 canary。
func (r *Repository) Overview(ctx context.Context) (Overview, error) {
	var o Overview
	var err error

	if o.Counts, err = r.counts(ctx); err != nil {
		return o, err
	}
	if o.Instances, err = r.instanceDist(ctx); err != nil {
		return o, err
	}
	if o.Nodes, err = r.nodeDist(ctx); err != nil {
		return o, err
	}
	if o.RunningCanary, err = r.runningCanary(ctx); err != nil {
		return o, err
	}
	if o.RunningBG, err = r.runningBG(ctx); err != nil {
		return o, err
	}
	return o, nil
}

// counts 逐个执行轻量 count（单次开销极小，管理端低频）。
func (r *Repository) counts(ctx context.Context) (Counts, error) {
	var c Counts
	items := []struct {
		dst *int
		q   func(context.Context) (int, error)
	}{
		{&c.Tenants, r.ent.Tenant.Query().Count},
		{&c.Domains, r.ent.Domain.Query().Count},
		{&c.Services, r.ent.Service.Query().Count},
		{&c.Instances, r.ent.Instance.Query().Count},
		{&c.Nodes, r.ent.Node.Query().Count},
		{&c.Deployments, r.ent.Deployment.Query().Count},
		{&c.Versions, r.ent.DeploymentVersion.Query().Count},
		{&c.Policies, r.ent.TrafficPolicy.Query().Count},
		{&c.RateLimits, r.ent.RateLimit.Query().Count},
		{&c.Canary, r.ent.CanaryRelease.Query().Count},
		{&c.BlueGreen, r.ent.BluegreenDeployment.Query().Count},
		{&c.Users, r.ent.User.Query().Count},
	}
	for _, it := range items {
		n, err := it.q(ctx)
		if err != nil {
			return c, fmt.Errorf("dashboard counts: %w", err)
		}
		*it.dst = n
	}
	return c, nil
}

// instanceDist 实例总数 / 可路由数（enabled+healthy）/ health 与 status 分布。
func (r *Repository) instanceDist(ctx context.Context) (Instances, error) {
	var d Instances
	d.ByHealth = map[string]int{}
	d.ByStatus = map[string]int{}

	total, err := r.ent.Instance.Query().Count(ctx)
	if err != nil {
		return d, fmt.Errorf("dashboard instances total: %w", err)
	}
	d.Total = total

	routable, err := r.ent.Instance.Query().
		Where(
			entinstance.StatusEQ("enabled"),
			entinstance.HealthEQ("healthy"),
		).
		Count(ctx)
	if err != nil {
		return d, fmt.Errorf("dashboard instances routable: %w", err)
	}
	d.Routable = routable

	for _, h := range []string{"healthy", "unhealthy", "unknown", "recovering"} {
		n, err := r.ent.Instance.Query().
			Where(entinstance.HealthEQ(h)).
			Count(ctx)
		if err != nil {
			return d, fmt.Errorf("dashboard instances health %s: %w", h, err)
		}
		d.ByHealth[h] = n
	}
	for _, s := range []string{"enabled", "disabled", "draining"} {
		n, err := r.ent.Instance.Query().
			Where(entinstance.StatusEQ(s)).
			Count(ctx)
		if err != nil {
			return d, fmt.Errorf("dashboard instances status %s: %w", s, err)
		}
		d.ByStatus[s] = n
	}
	return d, nil
}

// nodeDist 节点总数与状态分布。
func (r *Repository) nodeDist(ctx context.Context) (Nodes, error) {
	var d Nodes
	d.ByStatus = map[string]int{}
	type row struct {
		Status string `json:"status"`
		Count  int    `json:"count"`
	}
	var rows []row
	if err := r.ent.Node.Query().
		GroupBy(entnode.FieldStatus).
		Aggregate(func(s *sql.Selector) string { return sql.Count("*") }).
		Scan(ctx, &rows); err != nil {
		return d, fmt.Errorf("dashboard nodes: %w", err)
	}
	for _, r := range rows {
		d.Total += r.Count
		d.ByStatus[r.Status] = r.Count
	}
	return d, nil
}

// runningBG 进行中的 Blue/Green（active 已激活计数）。
func (r *Repository) runningBG(ctx context.Context) (int, error) {
	// 语义同旧 SQL：active_version_id IS NOT NULL 的行数。active_version_id 为非空字段时等价于整表行数。
	n, err := r.ent.BluegreenDeployment.Query().Count(ctx)
	if err != nil {
		return 0, fmt.Errorf("dashboard running bluegreen: %w", err)
	}
	return n, nil
}

// runningCanary 进行中 canary（created/running/paused）精简视图，附服务名。
func (r *Repository) runningCanary(ctx context.Context) ([]Canary, error) {
	es, err := r.ent.CanaryRelease.Query().
		Where(entcanary.PhaseIn("created", "running", "paused")).
		Order(entcanary.ByCreatedAt(sql.OrderDesc())).
		Limit(20).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("dashboard running canary: %w", err)
	}
	if len(es) == 0 {
		return []Canary{}, nil
	}

	// 批量取服务名（service_id → name），等价于 LEFT JOIN services。
	svcIDs := make([]uuid.UUID, 0, len(es))
	for _, e := range es {
		svcIDs = append(svcIDs, e.ServiceID)
	}
	svcs, err := r.ent.Service.Query().
		Where(entservice.IDIn(svcIDs...)).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("dashboard running canary services: %w", err)
	}
	svcName := map[uuid.UUID]string{}
	for _, s := range svcs {
		svcName[s.ID] = s.Name
	}

	out := make([]Canary, 0, len(es))
	for _, e := range es {
		out = append(out, Canary{
			ID:           e.ID.String(),
			ServiceID:    e.ServiceID.String(),
			ServiceName:  svcName[e.ServiceID],
			Name:         e.Name,
			Phase:        e.Phase,
			CanaryWeight: e.CanaryWeight,
			TargetWeight: e.TargetWeight,
		})
	}
	return out, nil
}
