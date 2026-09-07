package node

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/NeoPlayful/maple-gateway/server/pkg"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Repository 是 Node 数据访问层。
type Repository struct {
	pool *pgxpool.Pool
}

// NewRepository 构造。
func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

// COALESCE 兜底：region/labels 为 NULL 时归零值，保证 scan 不因 NULL 报错。
const cols = `id, name, host, COALESCE(region,'') AS region, COALESCE(labels,'{}'::jsonb) AS labels, status, weight, last_seen_at, created_at, updated_at`

func scanNode(row pgx.Row) (*Node, error) {
	var n Node
	err := row.Scan(&n.ID, &n.Name, &n.Host, &n.Region, &n.Labels, &n.Status, &n.Weight,
		&n.LastSeenAt, &n.CreatedAt, &n.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &n, nil
}

// Create 注册节点。name 冲突返回 Conflict。
func (r *Repository) Create(ctx context.Context, in New) (*Node, error) {
	weight := in.Weight
	if weight == 0 {
		weight = 1
	}
	row := r.pool.QueryRow(ctx, `
		INSERT INTO nodes(name, host, region, labels, status, weight)
		VALUES($1, $2, $3, $4, 'online', $5)
		RETURNING `+cols,
		in.Name, in.Host, in.Region, in.Labels, weight)
	n, err := scanNode(row)
	if err != nil {
		if pkg.IsUniqueViolation(err) {
			return nil, pkg.ErrConflict("节点名已存在")
		}
		return nil, fmt.Errorf("insert node: %w", err)
	}
	return n, nil
}

// GetByID 查询单个节点。
func (r *Repository) GetByID(ctx context.Context, id uuid.UUID) (*Node, error) {
	n, err := scanNode(r.pool.QueryRow(ctx, `SELECT `+cols+` FROM nodes WHERE id=$1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, pkg.ErrNotFound("节点不存在")
	}
	if err != nil {
		return nil, fmt.Errorf("get node: %w", err)
	}
	return n, nil
}

// RoutableMap 返回 node_id → 是否可接收流量（online 才可路由）。
// 供路由表重建过滤 offline/maintenance/disabled 节点上的实例。
func (r *Repository) RoutableMap(ctx context.Context) (map[uuid.UUID]bool, error) {
	rows, err := r.pool.Query(ctx, `SELECT id, status FROM nodes`)
	if err != nil {
		return nil, fmt.Errorf("node routable map: %w", err)
	}
	defer rows.Close()
	out := map[uuid.UUID]bool{}
	for rows.Next() {
		var id uuid.UUID
		var status Status
		if err := rows.Scan(&id, &status); err != nil {
			return nil, err
		}
		out[id] = status == StatusOnline
	}
	return out, rows.Err()
}

// All 返回全部节点（路由缓存构建 / 心跳扫描用）。
func (r *Repository) All(ctx context.Context) ([]*Node, error) {
	rows, err := r.pool.Query(ctx, `SELECT `+cols+` FROM nodes ORDER BY created_at`)
	if err != nil {
		return nil, fmt.Errorf("all nodes: %w", err)
	}
	defer rows.Close()
	out := []*Node{}
	for rows.Next() {
		n, err := scanNode(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

// List 分页列出节点，支持按状态筛选。
func (r *Repository) List(ctx context.Context, status Status, limit, offset int) ([]*Node, int, error) {
	var total int
	if err := r.pool.QueryRow(ctx, `SELECT count(*) FROM nodes WHERE ($1='' OR status=$1)`,
		string(status)).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count nodes: %w", err)
	}
	rows, err := r.pool.Query(ctx, `
		SELECT `+cols+` FROM nodes
		WHERE ($1='' OR status=$1)
		ORDER BY created_at DESC
		LIMIT $2 OFFSET $3`, string(status), limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("list nodes: %w", err)
	}
	defer rows.Close()
	out := []*Node{}
	for rows.Next() {
		n, err := scanNode(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, n)
	}
	return out, total, rows.Err()
}

// Update 应用非空更新（host/region/labels/weight/status）。
func (r *Repository) Update(ctx context.Context, id uuid.UUID, in Update) (*Node, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck // commit 成功后再 rollback 无害

	var n Node
	err = tx.QueryRow(ctx, `SELECT `+cols+` FROM nodes WHERE id=$1 FOR UPDATE`, id).
		Scan(&n.ID, &n.Name, &n.Host, &n.Region, &n.Labels, &n.Status, &n.Weight,
			&n.LastSeenAt, &n.CreatedAt, &n.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, pkg.ErrNotFound("节点不存在")
	}
	if err != nil {
		return nil, err
	}
	if in.Host != nil {
		n.Host = *in.Host
	}
	if in.Region != nil {
		n.Region = *in.Region
	}
	if in.Labels != nil {
		n.Labels = in.Labels
	}
	if in.Weight != nil {
		n.Weight = *in.Weight
	}
	if in.Status != nil {
		n.Status = *in.Status
	}

	err = tx.QueryRow(ctx, `
		UPDATE nodes SET host=$2, region=$3, labels=$4, status=$5, weight=$6, updated_at=now()
		WHERE id=$1 RETURNING `+cols,
		id, n.Host, n.Region, n.Labels, n.Status, n.Weight).
		Scan(&n.ID, &n.Name, &n.Host, &n.Region, &n.Labels, &n.Status, &n.Weight,
			&n.LastSeenAt, &n.CreatedAt, &n.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("update node: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &n, nil
}

// SetStatus 便捷状态变更（enable/disable/maintenance）。
func (r *Repository) SetStatus(ctx context.Context, id uuid.UUID, s Status) (*Node, error) {
	return r.Update(ctx, id, Update{Status: &s})
}

// Heartbeat 刷新节点最后心跳时间；非 disabled 节点心跳即恢复 online。
func (r *Repository) Heartbeat(ctx context.Context, id uuid.UUID) (*Node, error) {
	n, err := scanNode(r.pool.QueryRow(ctx, `
		UPDATE nodes SET status=CASE WHEN status='disabled' THEN status ELSE 'online' END,
			last_seen_at=now(), updated_at=now()
		WHERE id=$1 RETURNING `+cols, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, pkg.ErrNotFound("节点不存在")
	}
	if err != nil {
		return nil, fmt.Errorf("heartbeat node: %w", err)
	}
	return n, nil
}

// MarkOffline 把 last_seen_at 早于 cutoff（心跳超时）且当前非 disabled 的节点置为 offline。
// 返回被标记的节点数（供 watchdog 日志）。disabled 节点保持人工状态，不被覆盖。
func (r *Repository) MarkOffline(ctx context.Context, cutoff time.Time) (int64, error) {
	tag, err := r.pool.Exec(ctx, `
		UPDATE nodes SET status='offline', updated_at=now()
		WHERE status <> 'disabled'
		  AND (last_seen_at IS NULL OR last_seen_at < $1)`, cutoff)
	if err != nil {
		return 0, fmt.Errorf("mark nodes offline: %w", err)
	}
	return tag.RowsAffected(), nil
}

// Delete 删除节点，并清空其上实例的 node_id 引用（S3 起 node_id 才承载真实路由归属）。
func (r *Repository) Delete(ctx context.Context, id uuid.UUID) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	if _, err := tx.Exec(ctx, `UPDATE instances SET node_id=NULL, updated_at=now() WHERE node_id=$1`, id); err != nil {
		return fmt.Errorf("detach instances from node: %w", err)
	}
	tag, err := tx.Exec(ctx, `DELETE FROM nodes WHERE id=$1`, id)
	if err != nil {
		return fmt.Errorf("delete node: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return pkg.ErrNotFound("节点不存在")
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	return nil
}
