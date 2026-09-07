package instance

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strconv"

	"github.com/NeoPlayful/maple-gateway/server/pkg"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Repository 是 Instance 数据访问层。
type Repository struct {
	pool *pgxpool.Pool
}

// NewRepository 构造。
func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

const cols = `id, service_id, node_id, version, address, port, protocol, weight, status, health, last_seen_at, created_at, updated_at`

func scanInstance(row pgx.Row) (*Instance, error) {
	var i Instance
	err := row.Scan(&i.ID, &i.ServiceID, &i.NodeID, &i.Version, &i.Address, &i.Port, &i.Protocol,
		&i.Weight, &i.Status, &i.Health, &i.LastSeenAt, &i.CreatedAt, &i.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &i, nil
}

// Create 注册实例。
func (r *Repository) Create(ctx context.Context, in New) (*Instance, error) {
	proto := in.Protocol
	if proto == "" {
		proto = "http"
	}
	weight := in.Weight
	if weight == 0 {
		weight = 1
	}
	row := r.pool.QueryRow(ctx, `
		INSERT INTO instances(service_id, node_id, version, address, port, protocol, weight, status, health)
		VALUES($1, $2, $3, $4, $5, $6, $7, 'enabled', 'unknown')
		RETURNING `+cols,
		in.ServiceID, in.NodeID, in.Version, in.Address, in.Port, proto, weight)
	i, err := scanInstance(row)
	if err != nil {
		return nil, fmt.Errorf("insert instance: %w", err)
	}
	return i, nil
}

// GetByID 查询。
func (r *Repository) GetByID(ctx context.Context, id uuid.UUID) (*Instance, error) {
	i, err := scanInstance(r.pool.QueryRow(ctx, `SELECT `+cols+` FROM instances WHERE id=$1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, pkg.ErrNotFound("实例不存在")
	}
	if err != nil {
		return nil, fmt.Errorf("get instance: %w", err)
	}
	return i, nil
}

// ListByService 列出某服务的实例。
func (r *Repository) ListByService(ctx context.Context, serviceID uuid.UUID) ([]*Instance, error) {
	rows, err := r.pool.Query(ctx, `SELECT `+cols+` FROM instances WHERE service_id=$1 ORDER BY created_at`, serviceID)
	if err != nil {
		return nil, fmt.Errorf("list instances by service: %w", err)
	}
	defer rows.Close()
	return collect(rows)
}

// ListByServices 批量列出多个服务的实例（数据平面路由表构建用）。
func (r *Repository) ListByServices(ctx context.Context, serviceIDs []uuid.UUID) ([]*Instance, error) {
	if len(serviceIDs) == 0 {
		return []*Instance{}, nil
	}
	rows, err := r.pool.Query(ctx,
		`SELECT `+cols+` FROM instances WHERE service_id = ANY($1) ORDER BY service_id`,
		serviceIDs)
	if err != nil {
		return nil, fmt.Errorf("list instances by services: %w", err)
	}
	defer rows.Close()
	return collect(rows)
}

// AllGroupedByService 返回全部实例，按 service_id 分组（路由缓存构建用）。
func (r *Repository) AllGroupedByService(ctx context.Context) (map[uuid.UUID][]*Instance, error) {
	rows, err := r.pool.Query(ctx, `SELECT `+cols+` FROM instances`)
	if err != nil {
		return nil, fmt.Errorf("all instances grouped: %w", err)
	}
	defer rows.Close()
	out := map[uuid.UUID][]*Instance{}
	for rows.Next() {
		i, err := scanInstance(rows)
		if err != nil {
			return nil, err
		}
		out[i.ServiceID] = append(out[i.ServiceID], i)
	}
	return out, rows.Err()
}

// AllFlat 返回全部实例（健康检查扫描用）。
func (r *Repository) AllFlat(ctx context.Context) ([]*Instance, error) {
	rows, err := r.pool.Query(ctx, `SELECT `+cols+` FROM instances ORDER BY created_at`)
	if err != nil {
		return nil, fmt.Errorf("all instances flat: %w", err)
	}
	defer rows.Close()
	return collect(rows)
}

// ListAll 全量（分页 + 筛选）。
func (r *Repository) ListAll(ctx context.Context, limit, offset int) ([]*Instance, int, error) {
	var total int
	if err := r.pool.QueryRow(ctx, `SELECT count(*) FROM instances`).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := r.pool.Query(ctx, `SELECT `+cols+` FROM instances ORDER BY created_at DESC LIMIT $1 OFFSET $2`, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out, err := collect(rows)
	return out, total, err
}

// Update 应用非空更新。
func (r *Repository) Update(ctx context.Context, id uuid.UUID, in Update) (*Instance, error) {
	i, err := r.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if in.Address != nil {
		i.Address = *in.Address
	}
	if in.Port != nil {
		i.Port = *in.Port
	}
	if in.Protocol != nil {
		i.Protocol = *in.Protocol
	}
	if in.Weight != nil {
		i.Weight = *in.Weight
	}
	if in.Status != nil {
		i.Status = *in.Status
	}
	if in.Health != nil {
		i.Health = *in.Health
	}
	err = r.pool.QueryRow(ctx, `
		UPDATE instances SET address=$2, port=$3, protocol=$4, weight=$5,
			status=$6, health=$7, updated_at=now()
		WHERE id=$1 RETURNING `+cols,
		id, i.Address, i.Port, i.Protocol, i.Weight, i.Status, i.Health).
		Scan(&i.ID, &i.ServiceID, &i.NodeID, &i.Version, &i.Address, &i.Port, &i.Protocol,
			&i.Weight, &i.Status, &i.Health, &i.LastSeenAt, &i.CreatedAt, &i.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("update instance: %w", err)
	}
	return i, nil
}

// SetHealth 更新健康状态并刷新 last_seen。
func (r *Repository) SetHealth(ctx context.Context, id uuid.UUID, h Health) (*Instance, error) {
	i, err := r.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	err = r.pool.QueryRow(ctx, `
		UPDATE instances SET health=$2, last_seen_at=now(), updated_at=now()
		WHERE id=$1 RETURNING `+cols,
		id, h).Scan(&i.ID, &i.ServiceID, &i.NodeID, &i.Version, &i.Address, &i.Port, &i.Protocol,
		&i.Weight, &i.Status, &i.Health, &i.LastSeenAt, &i.CreatedAt, &i.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("set instance health: %w", err)
	}
	return i, nil
}

// Delete 注销实例。
func (r *Repository) Delete(ctx context.Context, id uuid.UUID) error {
	tag, err := r.pool.Exec(ctx, `DELETE FROM instances WHERE id=$1`, id)
	if err != nil {
		return fmt.Errorf("delete instance: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return pkg.ErrNotFound("实例不存在")
	}
	return nil
}

// RoutablePool 返回某服务当前可路由实例（healthy + enabled + 非 draining）。
func (r *Repository) RoutablePool(ctx context.Context, serviceID uuid.UUID) ([]*Instance, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT `+cols+` FROM instances
		WHERE service_id=$1 AND status='enabled' AND health='healthy'
		ORDER BY created_at`, serviceID)
	if err != nil {
		return nil, fmt.Errorf("query routable pool: %w", err)
	}
	defer rows.Close()
	return collect(rows)
}

func collect(rows pgx.Rows) ([]*Instance, error) {
	out := []*Instance{}
	for rows.Next() {
		i, err := scanInstance(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, i)
	}
	return out, rows.Err()
}

// addrJoin 拼 host:port（IPv6 兼容）。
func addrJoin(address string, port int) string {
	return net.JoinHostPort(address, strconv.Itoa(port))
}
