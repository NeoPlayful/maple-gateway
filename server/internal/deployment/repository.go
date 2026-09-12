package deployment

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"entgo.io/ent/dialect/sql"
	"github.com/NeoPlayful/maple-gateway/server/ent"
	entdeployment "github.com/NeoPlayful/maple-gateway/server/ent/deployment"
	entdv "github.com/NeoPlayful/maple-gateway/server/ent/deploymentversion"
	entinstance "github.com/NeoPlayful/maple-gateway/server/ent/instance"
	"github.com/NeoPlayful/maple-gateway/server/pkg"
	"github.com/google/uuid"
)

// nonNilEnv 保证 JSONB map 写入非 nil（ent 对 nil map 写 NULL，空 map 更便于查询）。
func nonNilEnv(m map[string]string) map[string]string {
	if m == nil {
		return map[string]string{}
	}
	return m
}

// nonNilRaw 保证 JSONB RawMessage 写入非 nil。
func nonNilRaw(m json.RawMessage) json.RawMessage {
	if m == nil {
		return json.RawMessage(`{}`)
	}
	return m
}

// Repository 是 Deployment / Version 数据访问层（基于 Ent）。
type Repository struct {
	ent *ent.Client
}

// NewRepository 构造。
func NewRepository(client *ent.Client) *Repository {
	return &Repository{ent: client}
}

func toDeployment(e *ent.Deployment) *Deployment {
	return &Deployment{
		ID:        e.ID,
		ServiceID: e.ServiceID,
		Name:      e.Name,
		Status:    Status(e.Status),
		Strategy:  Strategy(e.Strategy),
		CreatedAt: e.CreatedAt,
		UpdatedAt: e.UpdatedAt,
	}
}

func toVersion(e *ent.DeploymentVersion) *Version {
	return &Version{
		ID:           e.ID,
		DeploymentID: e.DeploymentID,
		Version:      e.Version,
		Image:        e.Image,
		Weight:       e.Weight,
		Status:       VersionStatus(e.Status),
		Replicas:     e.Replicas,
		Port:         e.Port,
		Env:          e.Env,
		Resources:    json.RawMessage(e.Resources),
		HealthPath:   e.HealthPath,
		NodeSelector: e.NodeSelector,
		CreatedAt:    e.CreatedAt,
		UpdatedAt:    e.UpdatedAt,
	}
}

// ---------- Deployment ----------

// CreateDeployment 插入部署。service_id 不存在走外键错误，同名冲突走唯一冲突。
func (r *Repository) CreateDeployment(ctx context.Context, in NewDeployment) (*Deployment, error) {
	strategy := in.Strategy
	if strategy == "" {
		strategy = StrategyRolling
	}
	now := time.Now()
	e, err := r.ent.Deployment.Create().
		SetServiceID(in.ServiceID).
		SetName(in.Name).
		SetStatus(string(StatusActive)).
		SetStrategy(string(strategy)).
		SetCreatedAt(now).
		SetUpdatedAt(now).
		Save(ctx)
	if err != nil {
		if pkg.IsUniqueViolation(err) {
			return nil, pkg.ErrConflict("该服务下已存在同名部署")
		}
		if pkg.IsForeignKeyViolation(err) {
			return nil, pkg.ErrValidation("服务不存在")
		}
		return nil, fmt.Errorf("insert deployment: %w", err)
	}
	return toDeployment(e), nil
}

// GetDeployment 查询部署。
func (r *Repository) GetDeployment(ctx context.Context, id uuid.UUID) (*Deployment, error) {
	e, err := r.ent.Deployment.Get(ctx, id)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, pkg.ErrNotFound("部署不存在")
		}
		return nil, fmt.Errorf("get deployment: %w", err)
	}
	return toDeployment(e), nil
}

// ListDeployments 分页列出部署，支持 service_id 筛选（nil 表示全部）。
func (r *Repository) ListDeployments(ctx context.Context, serviceID *uuid.UUID, limit, offset int) ([]*Deployment, int, error) {
	q := r.ent.Deployment.Query()
	if serviceID != nil {
		q = q.Where(entdeployment.ServiceID(*serviceID))
	}
	total, err := q.Count(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("count deployments: %w", err)
	}
	es, err := q.
		Order(entdeployment.ByCreatedAt(sql.OrderDesc())).
		Limit(limit).
		Offset(offset).
		All(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("list deployments: %w", err)
	}
	out := make([]*Deployment, 0, len(es))
	for _, e := range es {
		out = append(out, toDeployment(e))
	}
	return out, total, nil
}

// UpdateDeployment 应用非空更新。
func (r *Repository) UpdateDeployment(ctx context.Context, id uuid.UUID, in UpdateDeployment) (*Deployment, error) {
	if _, err := r.ent.Deployment.Get(ctx, id); err != nil {
		if ent.IsNotFound(err) {
			return nil, pkg.ErrNotFound("部署不存在")
		}
		return nil, fmt.Errorf("get deployment for update: %w", err)
	}
	upd := r.ent.Deployment.UpdateOneID(id).SetUpdatedAt(time.Now())
	if in.Name != nil {
		upd = upd.SetName(*in.Name)
	}
	if in.Status != nil {
		upd = upd.SetStatus(string(*in.Status))
	}
	if in.Strategy != nil {
		upd = upd.SetStrategy(string(*in.Strategy))
	}
	e, err := upd.Save(ctx)
	if err != nil {
		if pkg.IsUniqueViolation(err) {
			return nil, pkg.ErrConflict("该服务下已存在同名部署")
		}
		return nil, fmt.Errorf("update deployment: %w", err)
	}
	return toDeployment(e), nil
}

// SetDeploymentStatus 便捷状态变更。
func (r *Repository) SetDeploymentStatus(ctx context.Context, id uuid.UUID, s Status) (*Deployment, error) {
	return r.UpdateDeployment(ctx, id, UpdateDeployment{Status: &s})
}

// DeleteDeployment 删除部署（级联删除版本；其上实例的 deployment_id 置空，同事务）。
func (r *Repository) DeleteDeployment(ctx context.Context, id uuid.UUID) error {
	tx, err := r.ent.Tx(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.Instance.Update().
		Where(entinstance.DeploymentID(id)).
		ClearDeploymentID().
		Save(ctx); err != nil {
		return fmt.Errorf("detach instances from deployment: %w", err)
	}
	err = tx.Deployment.DeleteOneID(id).Exec(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return pkg.ErrNotFound("部署不存在")
		}
		return fmt.Errorf("delete deployment: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit delete deployment: %w", err)
	}
	return nil
}

// ---------- Version ----------

// CreateVersion 在部署下新增版本。同 deployment 内版本号唯一。
func (r *Repository) CreateVersion(ctx context.Context, deploymentID uuid.UUID, in NewVersion) (*Version, error) {
	weight := in.Weight
	if weight == 0 {
		weight = 100
	}
	status := in.Status
	if status == "" {
		status = VersionStable
	}
	replicas := in.Replicas
	if replicas == 0 {
		replicas = 1
	}
	now := time.Now()
	e, err := r.ent.DeploymentVersion.Create().
		SetDeploymentID(deploymentID).
		SetVersion(in.Version).
		SetImage(in.Image).
		SetWeight(weight).
		SetStatus(string(status)).
		SetReplicas(replicas).
		SetPort(in.Port).
		SetEnv(nonNilEnv(in.Env)).
		SetResources(nonNilRaw(in.Resources)).
		SetHealthPath(in.HealthPath).
		SetNodeSelector(nonNilEnv(in.NodeSelector)).
		SetCreatedAt(now).
		SetUpdatedAt(now).
		Save(ctx)
	if err != nil {
		if pkg.IsUniqueViolation(err) {
			return nil, pkg.ErrConflict("该部署下已存在同名版本")
		}
		if pkg.IsForeignKeyViolation(err) {
			return nil, pkg.ErrValidation("部署不存在")
		}
		return nil, fmt.Errorf("insert version: %w", err)
	}
	return toVersion(e), nil
}

// GetVersion 查询版本。
func (r *Repository) GetVersion(ctx context.Context, id uuid.UUID) (*Version, error) {
	e, err := r.ent.DeploymentVersion.Get(ctx, id)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, pkg.ErrNotFound("版本不存在")
		}
		return nil, fmt.Errorf("get version: %w", err)
	}
	return toVersion(e), nil
}

// ListVersions 列出某部署的全部版本。
func (r *Repository) ListVersions(ctx context.Context, deploymentID uuid.UUID) ([]*Version, error) {
	es, err := r.ent.DeploymentVersion.Query().
		Where(entdv.DeploymentID(deploymentID)).
		Order(entdv.ByCreatedAt()).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("list versions: %w", err)
	}
	out := make([]*Version, 0, len(es))
	for _, e := range es {
		out = append(out, toVersion(e))
	}
	return out, nil
}

// ListVersionsByDeployments 批量列出多个部署的版本（路由表构建用）。
func (r *Repository) ListVersionsByDeployments(ctx context.Context, deploymentIDs []uuid.UUID) ([]*Version, error) {
	if len(deploymentIDs) == 0 {
		return []*Version{}, nil
	}
	es, err := r.ent.DeploymentVersion.Query().
		Where(entdv.DeploymentIDIn(deploymentIDs...)).
		Order(entdv.ByDeploymentID(), entdv.ByCreatedAt()).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("list versions by deployments: %w", err)
	}
	out := make([]*Version, 0, len(es))
	for _, e := range es {
		out = append(out, toVersion(e))
	}
	return out, nil
}

// UpdateVersion 应用非空更新（image/weight/status）。
func (r *Repository) UpdateVersion(ctx context.Context, id uuid.UUID, in UpdateVersion) (*Version, error) {
	if _, err := r.ent.DeploymentVersion.Get(ctx, id); err != nil {
		if ent.IsNotFound(err) {
			return nil, pkg.ErrNotFound("版本不存在")
		}
		return nil, fmt.Errorf("get version for update: %w", err)
	}
	upd := r.ent.DeploymentVersion.UpdateOneID(id).SetUpdatedAt(time.Now())
	if in.Image != nil {
		upd = upd.SetImage(*in.Image)
	}
	if in.Weight != nil {
		upd = upd.SetWeight(*in.Weight)
	}
	if in.Status != nil {
		upd = upd.SetStatus(string(*in.Status))
	}
	if in.Replicas != nil {
		upd = upd.SetReplicas(*in.Replicas)
	}
	if in.Port != nil {
		upd = upd.SetPort(*in.Port)
	}
	if in.Env != nil {
		upd = upd.SetEnv(in.Env)
	}
	if in.Resources != nil {
		upd = upd.SetResources(in.Resources)
	}
	if in.HealthPath != nil {
		upd = upd.SetHealthPath(*in.HealthPath)
	}
	if in.NodeSelector != nil {
		upd = upd.SetNodeSelector(in.NodeSelector)
	}
	e, err := upd.Save(ctx)
	if err != nil {
		return nil, fmt.Errorf("update version: %w", err)
	}
	return toVersion(e), nil
}

// SetVersionStatus 便捷状态变更。
func (r *Repository) SetVersionStatus(ctx context.Context, id uuid.UUID, s VersionStatus) (*Version, error) {
	return r.UpdateVersion(ctx, id, UpdateVersion{Status: &s})
}

// DeleteVersion 删除版本，并清空引用它的实例的 version_id（同事务，避免路由漂移）。
func (r *Repository) DeleteVersion(ctx context.Context, id uuid.UUID) error {
	tx, err := r.ent.Tx(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.Instance.Update().
		Where(entinstance.VersionID(id)).
		ClearVersionID().
		Save(ctx); err != nil {
		return fmt.Errorf("detach instances from version: %w", err)
	}
	err = tx.DeploymentVersion.DeleteOneID(id).Exec(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return pkg.ErrNotFound("版本不存在")
		}
		return fmt.Errorf("delete version: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit delete version: %w", err)
	}
	return nil
}

// VersionGroupRow 是跨 service 的版本聚合行（路由表构建用，避免引入 cache 依赖）。
type VersionGroupRow struct {
	ServiceID    uuid.UUID
	DeploymentID uuid.UUID
	VersionID    uuid.UUID
	Version      string
	Weight       int
	Status       VersionStatus
}

// AllVersionGroups 返回全部 deployments × deployment_versions 的聚合行，供路由表构建。
func (r *Repository) AllVersionGroups(ctx context.Context) ([]VersionGroupRow, error) {
	vs, err := r.ent.DeploymentVersion.Query().
		WithDeployment().
		Order(entdv.ByCreatedAt()).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("all version groups: %w", err)
	}
	out := make([]VersionGroupRow, 0, len(vs))
	for _, v := range vs {
		if v.Edges.Deployment == nil {
			continue
		}
		out = append(out, VersionGroupRow{
			ServiceID:    v.Edges.Deployment.ServiceID,
			DeploymentID: v.DeploymentID,
			VersionID:    v.ID,
			Version:      v.Version,
			Weight:       v.Weight,
			Status:       VersionStatus(v.Status),
		})
	}
	return out, nil
}

// SetDefaultVersion 将部署内某版本设为 stable，其余 stable 降为 standby（事务）。
func (r *Repository) SetDefaultVersion(ctx context.Context, deploymentID, versionID uuid.UUID) (*Version, error) {
	tx, err := r.ent.Tx(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	cur, err := tx.DeploymentVersion.Get(ctx, versionID)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, pkg.ErrNotFound("版本不存在")
		}
		return nil, err
	}
	if cur.DeploymentID != deploymentID {
		return nil, pkg.ErrValidation("版本不属于该部署")
	}

	// 同部署内其余可路由版本 → standby，保证仅一个 stable。
	if _, err := tx.DeploymentVersion.Update().
		Where(
			entdv.DeploymentID(deploymentID),
			entdv.IDNEQ(versionID),
			entdv.StatusIn("stable", "canary", "active", "standby", "draining"),
		).
		SetStatus(string(VersionStandby)).
		SetUpdatedAt(time.Now()).
		Save(ctx); err != nil {
		return nil, fmt.Errorf("demote old versions: %w", err)
	}
	e, err := tx.DeploymentVersion.UpdateOneID(versionID).
		SetStatus(string(VersionStable)).
		SetUpdatedAt(time.Now()).
		Save(ctx)
	if err != nil {
		return nil, fmt.Errorf("promote version: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit set default version: %w", err)
	}
	return toVersion(e), nil
}
