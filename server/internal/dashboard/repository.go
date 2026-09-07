package dashboard

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Repository 提供 Dashboard 只读聚合查询。
type Repository struct {
	pool *pgxpool.Pool
}

// NewRepository 构造。
func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
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
	if o.RunningBG, err = r.countWhere(ctx,
		`SELECT count(*) FROM bluegreen_deployments WHERE active_version_id IS NOT NULL`); err != nil {
		return o, err
	}
	return o, nil
}

// counts 逐个执行轻量 count（单次开销极小，管理端低频）。
func (r *Repository) counts(ctx context.Context) (Counts, error) {
	var c Counts
	items := []struct {
		dst *int
		sql string
	}{
		{&c.Tenants, "SELECT count(*) FROM tenants"},
		{&c.Domains, "SELECT count(*) FROM domains"},
		{&c.Services, "SELECT count(*) FROM services"},
		{&c.Instances, "SELECT count(*) FROM instances"},
		{&c.Nodes, "SELECT count(*) FROM nodes"},
		{&c.Deployments, "SELECT count(*) FROM deployments"},
		{&c.Versions, "SELECT count(*) FROM deployment_versions"},
		{&c.Policies, "SELECT count(*) FROM traffic_policies"},
		{&c.RateLimits, "SELECT count(*) FROM rate_limits"},
		{&c.Canary, "SELECT count(*) FROM canary_releases"},
		{&c.BlueGreen, "SELECT count(*) FROM bluegreen_deployments"},
		{&c.Admins, "SELECT count(*) FROM admins"},
	}
	for _, it := range items {
		n, err := r.countWhere(ctx, it.sql)
		if err != nil {
			return c, fmt.Errorf("dashboard counts: %w", err)
		}
		*it.dst = n
	}
	return c, nil
}

// instanceDist 实例总数 / 可路由数（enabled+healthy）/ health 与 status 分布。
func (r *Repository) instanceDist(ctx context.Context) (Instances, error) {
	var (
		d                     Instances
		hHealthy, hUnhealthy  int
		hUnknown, hRecovering int
		sEnabled, sDisabled   int
		sDraining             int
	)
	d.ByHealth = map[string]int{}
	d.ByStatus = map[string]int{}
	err := r.pool.QueryRow(ctx, `
		SELECT
			count(*) AS total,
			count(*) FILTER (WHERE status='enabled' AND health='healthy') AS routable,
			count(*) FILTER (WHERE health='healthy'),
			count(*) FILTER (WHERE health='unhealthy'),
			count(*) FILTER (WHERE health='unknown'),
			count(*) FILTER (WHERE health='recovering'),
			count(*) FILTER (WHERE status='enabled'),
			count(*) FILTER (WHERE status='disabled'),
			count(*) FILTER (WHERE status='draining')
		FROM instances`).Scan(&d.Total, &d.Routable,
		&hHealthy, &hUnhealthy, &hUnknown, &hRecovering,
		&sEnabled, &sDisabled, &sDraining)
	if err != nil {
		return d, fmt.Errorf("dashboard instances: %w", err)
	}
	d.ByHealth["healthy"] = hHealthy
	d.ByHealth["unhealthy"] = hUnhealthy
	d.ByHealth["unknown"] = hUnknown
	d.ByHealth["recovering"] = hRecovering
	d.ByStatus["enabled"] = sEnabled
	d.ByStatus["disabled"] = sDisabled
	d.ByStatus["draining"] = sDraining
	return d, nil
}

// nodeDist 节点总数与状态分布。
func (r *Repository) nodeDist(ctx context.Context) (Nodes, error) {
	var d Nodes
	d.ByStatus = map[string]int{}
	rows, err := r.pool.Query(ctx,
		`SELECT status, count(*) FROM nodes GROUP BY status`)
	if err != nil {
		return d, fmt.Errorf("dashboard nodes: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var status string
		var n int
		if err := rows.Scan(&status, &n); err != nil {
			return d, err
		}
		d.Total += n
		d.ByStatus[status] = n
	}
	return d, rows.Err()
}

// runningCanary 进行中 canary（created/running/paused）精简视图，附服务名。
func (r *Repository) runningCanary(ctx context.Context) ([]Canary, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT cr.id, cr.service_id, s.name AS service_name, cr.name,
			cr.phase, cr.canary_weight, cr.target_weight
		FROM canary_releases cr
		LEFT JOIN services s ON s.id = cr.service_id
		WHERE cr.phase IN ('created','running','paused')
		ORDER BY cr.created_at DESC LIMIT 20`)
	if err != nil {
		return nil, fmt.Errorf("dashboard running canary: %w", err)
	}
	defer rows.Close()
	out := []Canary{}
	for rows.Next() {
		var c Canary
		if err := rows.Scan(&c.ID, &c.ServiceID, &c.ServiceName, &c.Name,
			&c.Phase, &c.CanaryWeight, &c.TargetWeight); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (r *Repository) countWhere(ctx context.Context, sql string) (int, error) {
	var n int
	if err := r.pool.QueryRow(ctx, sql).Scan(&n); err != nil {
		return 0, err
	}
	return n, nil
}
