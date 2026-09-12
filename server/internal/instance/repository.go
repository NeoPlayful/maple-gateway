package instance

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"time"

	"entgo.io/ent/dialect/sql"
	"github.com/NeoPlayful/maple-gateway/server/ent"
	entinstance "github.com/NeoPlayful/maple-gateway/server/ent/instance"
	"github.com/NeoPlayful/maple-gateway/server/internal/security"
	"github.com/NeoPlayful/maple-gateway/server/pkg"
	"github.com/google/uuid"
)

// Repository 是 Instance 数据访问层。
type Repository struct {
	ent *ent.Client
}

// NewRepository 构造。
func NewRepository(client *ent.Client) *Repository {
	return &Repository{ent: client}
}

// toModel 把 Ent 实体映射为领域模型。
func toModel(e *ent.Instance) *Instance {
	return &Instance{
		ID:           e.ID,
		ServiceID:    e.ServiceID,
		DeploymentID: e.DeploymentID,
		VersionID:    e.VersionID,
		NodeID:       e.NodeID,
		Version:      e.Version,
		Address:      e.Address,
		Port:         e.Port,
		Protocol:     e.Protocol,
		Weight:       e.Weight,
		Status:       Status(e.Status),
		Health:       Health(e.Health),
		LastSeenAt:   e.LastSeenAt,
		CreatedAt:    e.CreatedAt,
		UpdatedAt:    e.UpdatedAt,
	}
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
	// 兜底 SSRF 校验：Create 是唯一入库入口，discovery/admin 均经此，防写库绕过。
	if err := security.ValidateUpstreamAddress(in.Address); err != nil {
		return nil, err
	}
	if err := r.validateMount(ctx, in.ServiceID, in.DeploymentID, in.VersionID, in.NodeID); err != nil {
		return nil, err
	}
	now := time.Now()
	create := r.ent.Instance.Create().
		SetServiceID(in.ServiceID).
		SetNillableDeploymentID(in.DeploymentID).
		SetNillableVersionID(in.VersionID).
		SetNillableNodeID(in.NodeID).
		SetVersion(in.Version).
		SetAddress(in.Address).
		SetPort(in.Port).
		SetProtocol(proto).
		SetWeight(weight).
		SetStatus(string(StatusEnabled)).
		SetHealth(string(HealthUnknown)).
		SetLastSeenAt(now). // 注册即视为被 CM 看见
		SetCreatedAt(now).
		SetUpdatedAt(now)
	// 显式 ID：CM 上报时用容器 maple.instance_id 标签，保证与实例 ID 一一对应。
	if in.ID != nil {
		create = create.SetID(*in.ID)
	}
	e, err := create.Save(ctx)
	if err != nil {
		if pkg.IsUniqueViolation(err) {
			return nil, pkg.ErrConflict("实例 ID 已存在")
		}
		return nil, fmt.Errorf("insert instance: %w", err)
	}
	return toModel(e), nil
}

// GetByID 查询。
func (r *Repository) GetByID(ctx context.Context, id uuid.UUID) (*Instance, error) {
	e, err := r.ent.Instance.Get(ctx, id)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, pkg.ErrNotFound("实例不存在")
		}
		return nil, fmt.Errorf("get instance: %w", err)
	}
	return toModel(e), nil
}

// ListByService 列出某服务的实例。
func (r *Repository) ListByService(ctx context.Context, serviceID uuid.UUID) ([]*Instance, error) {
	es, err := r.ent.Instance.Query().
		Where(entinstance.ServiceID(serviceID)).
		Order(entinstance.ByCreatedAt()).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("list instances by service: %w", err)
	}
	out := make([]*Instance, 0, len(es))
	for _, e := range es {
		out = append(out, toModel(e))
	}
	return out, nil
}

// ListByServices 批量列出多个服务的实例（数据平面路由表构建用）。
func (r *Repository) ListByServices(ctx context.Context, serviceIDs []uuid.UUID) ([]*Instance, error) {
	if len(serviceIDs) == 0 {
		return []*Instance{}, nil
	}
	es, err := r.ent.Instance.Query().
		Where(entinstance.ServiceIDIn(serviceIDs...)).
		Order(entinstance.ByServiceID()).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("list instances by services: %w", err)
	}
	out := make([]*Instance, 0, len(es))
	for _, e := range es {
		out = append(out, toModel(e))
	}
	return out, nil
}

// AllGroupedByService 返回全部实例，按 service_id 分组（路由缓存构建用）。
func (r *Repository) AllGroupedByService(ctx context.Context) (map[uuid.UUID][]*Instance, error) {
	es, err := r.ent.Instance.Query().All(ctx)
	if err != nil {
		return nil, fmt.Errorf("all instances grouped: %w", err)
	}
	out := map[uuid.UUID][]*Instance{}
	for _, e := range es {
		m := toModel(e)
		out[m.ServiceID] = append(out[m.ServiceID], m)
	}
	return out, nil
}

// AllFlat 返回全部实例（健康检查扫描用）。
func (r *Repository) AllFlat(ctx context.Context) ([]*Instance, error) {
	es, err := r.ent.Instance.Query().Order(entinstance.ByCreatedAt()).All(ctx)
	if err != nil {
		return nil, fmt.Errorf("all instances flat: %w", err)
	}
	out := make([]*Instance, 0, len(es))
	for _, e := range es {
		out = append(out, toModel(e))
	}
	return out, nil
}

// ListAll 全量（分页 + 筛选）。
func (r *Repository) ListAll(ctx context.Context, limit, offset int) ([]*Instance, int, error) {
	total, err := r.ent.Instance.Query().Count(ctx)
	if err != nil {
		return nil, 0, err
	}
	es, err := r.ent.Instance.Query().
		Order(entinstance.ByCreatedAt(sql.OrderDesc())).
		Limit(limit).
		Offset(offset).
		All(ctx)
	if err != nil {
		return nil, 0, err
	}
	out := make([]*Instance, 0, len(es))
	for _, e := range es {
		out = append(out, toModel(e))
	}
	return out, total, nil
}

// Update 应用非空更新。
func (r *Repository) Update(ctx context.Context, id uuid.UUID, in Update) (*Instance, error) {
	cur, err := r.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	address := cur.Address
	if in.Address != nil {
		address = *in.Address
	}
	port := cur.Port
	if in.Port != nil {
		port = *in.Port
	}
	protocol := cur.Protocol
	if in.Protocol != nil {
		protocol = *in.Protocol
	}
	weight := cur.Weight
	if in.Weight != nil {
		weight = *in.Weight
	}
	status := cur.Status
	if in.Status != nil {
		status = *in.Status
	}
	health := cur.Health
	if in.Health != nil {
		health = *in.Health
	}
	// address 变更同样过 SSRF 校验，堵住 PATCH/discovery 改地址绕过。
	if in.Address != nil {
		if err := security.ValidateUpstreamAddress(address); err != nil {
			return nil, err
		}
	}
	upd := r.ent.Instance.UpdateOneID(id).
		SetAddress(address).
		SetPort(port).
		SetProtocol(protocol).
		SetWeight(weight).
		SetStatus(string(status)).
		SetHealth(string(health)).
		SetUpdatedAt(time.Now())
	e, err := upd.Save(ctx)
	if err != nil {
		return nil, fmt.Errorf("update instance: %w", err)
	}
	return toModel(e), nil
}

// SetHealth 更新健康状态。
func (r *Repository) SetHealth(ctx context.Context, id uuid.UUID, h Health) (*Instance, error) {
	e, err := r.ent.Instance.UpdateOneID(id).
		SetHealth(string(h)).
		SetUpdatedAt(time.Now()).
		Save(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, pkg.ErrNotFound("实例不存在")
		}
		return nil, fmt.Errorf("set instance health: %w", err)
	}
	return toModel(e), nil
}

// Heartbeat 刷新实例"最后被 CM 看见"时间（last_seen_at），不改变状态与健康。
func (r *Repository) Heartbeat(ctx context.Context, id uuid.UUID) (*Instance, error) {
	now := time.Now()
	e, err := r.ent.Instance.UpdateOneID(id).
		SetLastSeenAt(now).
		SetUpdatedAt(now).
		Save(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, pkg.ErrNotFound("实例不存在")
		}
		return nil, fmt.Errorf("instance heartbeat: %w", err)
	}
	return toModel(e), nil
}

// Drain 把实例置为 draining（停止接收新流量），保留现有健康与 last_seen。
func (r *Repository) Drain(ctx context.Context, id uuid.UUID) (*Instance, error) {
	e, err := r.ent.Instance.UpdateOneID(id).
		SetStatus(string(StatusDraining)).
		SetUpdatedAt(time.Now()).
		Save(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, pkg.ErrNotFound("实例不存在")
		}
		return nil, fmt.Errorf("drain instance: %w", err)
	}
	return toModel(e), nil
}

// Mount 调整实例挂载。DeploymentID/VersionID 为 nil 不动，uuid.Nil 解挂（回退直挂 Service）。
// version 有值但 deployment 无值视为非法。
func (r *Repository) Mount(ctx context.Context, id uuid.UUID, in Mount) (*Instance, error) {
	cur, err := r.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}

	var newDep, newVer, newNode *uuid.UUID
	newDep, newVer, newNode = cur.DeploymentID, cur.VersionID, cur.NodeID
	if in.DeploymentID != nil {
		if *in.DeploymentID == uuid.Nil {
			newDep, newVer = nil, nil // 解挂 deployment 连带清 version
		} else {
			newDep = in.DeploymentID
			if in.VersionID == nil {
				newVer = nil // 换 deployment 不清 version，但需校验归属
			}
		}
	}
	if in.VersionID != nil {
		if *in.VersionID == uuid.Nil {
			newVer = nil
		} else {
			newVer = in.VersionID
		}
	}
	if in.NodeID != nil {
		if *in.NodeID == uuid.Nil {
			newNode = nil
		} else {
			newNode = in.NodeID
		}
	}

	if err := r.validateMount(ctx, cur.ServiceID, newDep, newVer, newNode); err != nil {
		return nil, err
	}
	upd := r.ent.Instance.UpdateOneID(id).
		SetNillableDeploymentID(newDep).
		SetNillableVersionID(newVer).
		SetNillableNodeID(newNode).
		SetUpdatedAt(time.Now())
	e, err := upd.Save(ctx)
	if err != nil {
		return nil, fmt.Errorf("mount instance: %w", err)
	}
	return toModel(e), nil
}

// Delete 注销实例。
func (r *Repository) Delete(ctx context.Context, id uuid.UUID) error {
	err := r.ent.Instance.DeleteOneID(id).Exec(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return pkg.ErrNotFound("实例不存在")
		}
		return fmt.Errorf("delete instance: %w", err)
	}
	return nil
}

// RoutablePool 返回某服务当前可路由实例（healthy + enabled + 非 draining）。
func (r *Repository) RoutablePool(ctx context.Context, serviceID uuid.UUID) ([]*Instance, error) {
	es, err := r.ent.Instance.Query().
		Where(
			entinstance.ServiceID(serviceID),
			entinstance.StatusEQ(string(StatusEnabled)),
			entinstance.HealthEQ(string(HealthHealthy)),
		).
		Order(entinstance.ByCreatedAt()).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("query routable pool: %w", err)
	}
	out := make([]*Instance, 0, len(es))
	for _, e := range es {
		out = append(out, toModel(e))
	}
	return out, nil
}

// validateMount 校验挂载一致性：version 必须属于 deployment；version 有值而 deployment 为空非法；
// deployment 有值而 version 为空允许（Phase 1 直挂迁移期）。resource 存在性由外键保证。
func (r *Repository) validateMount(ctx context.Context, serviceID uuid.UUID,
	deploymentID, versionID, nodeID *uuid.UUID) error {
	if versionID == nil {
		return nil
	}
	if deploymentID == nil {
		return pkg.ErrValidation("指定 version_id 时必须同时指定 deployment_id")
	}
	v, err := r.ent.DeploymentVersion.Get(ctx, *versionID)
	if err != nil {
		if ent.IsNotFound(err) {
			return pkg.ErrValidation("版本不存在")
		}
		return fmt.Errorf("check version: %w", err)
	}
	if v.DeploymentID != *deploymentID {
		return pkg.ErrValidation("version_id 不属于该 deployment")
	}
	return nil
}

// addrJoin 拼 host:port（IPv6 兼容）。
func addrJoin(address string, port int) string {
	return net.JoinHostPort(address, strconv.Itoa(port))
}
