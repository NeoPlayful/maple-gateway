package certificate

import (
	"context"
	"fmt"
	"time"

	"github.com/NeoPlayful/maple-gateway/server/ent"
	entcert "github.com/NeoPlayful/maple-gateway/server/ent/certificate"
	"github.com/NeoPlayful/maple-gateway/server/pkg"
	"github.com/google/uuid"
)

// Repository 是 Certificate 数据访问层（基于 Ent）。
type Repository struct {
	ent *ent.Client
}

// NewRepository 构造。
func NewRepository(client *ent.Client) *Repository {
	return &Repository{ent: client}
}

// toModel 把 Ent 实体映射为领域模型（密文字段不进 JSON）。
func toModel(e *ent.Certificate) *Certificate {
	return &Certificate{
		ID:                  e.ID,
		DomainID:            e.DomainID,
		Hostname:            e.Hostname,
		Source:              Source(e.Source),
		Status:              Status(e.Status),
		CertificatePEM:      e.CertificatePem,
		PrivateKeyEncrypted: e.PrivateKeyEncrypted,
		Issuer:              e.Issuer,
		SerialNumber:        e.SerialNumber,
		IssuedAt:            e.IssuedAt,
		ExpiresAt:           e.ExpiresAt,
		LastRenewedAt:       e.LastRenewedAt,
		LastError:           e.LastError,
		CreatedAt:           e.CreatedAt,
		UpdatedAt:           e.UpdatedAt,
	}
}

// Create 插入证书。hostname 冲突（同 hostname 已有 active 证书）由 Service 决定是否替换。
func (r *Repository) Create(ctx context.Context, in *Certificate) (*Certificate, error) {
	now := time.Now()
	cr := r.ent.Certificate.Create().
		SetHostname(in.Hostname).
		SetSource(string(in.Source)).
		SetStatus(string(in.Status)).
		SetCertificatePem(in.CertificatePEM).
		SetPrivateKeyEncrypted(in.PrivateKeyEncrypted).
		SetCreatedAt(now).
		SetUpdatedAt(now)
	if in.DomainID != nil {
		cr = cr.SetDomainID(*in.DomainID)
	}
	if in.Issuer != "" {
		cr = cr.SetIssuer(in.Issuer)
	}
	if in.SerialNumber != "" {
		cr = cr.SetSerialNumber(in.SerialNumber)
	}
	if in.IssuedAt != nil {
		cr = cr.SetIssuedAt(*in.IssuedAt)
	}
	if in.ExpiresAt != nil {
		cr = cr.SetExpiresAt(*in.ExpiresAt)
	}
	if in.LastError != "" {
		cr = cr.SetLastError(in.LastError)
	}
	e, err := cr.Save(ctx)
	if err != nil {
		if pkg.IsUniqueViolation(err) {
			return nil, pkg.ErrConflict("hostname 已存在证书")
		}
		return nil, fmt.Errorf("insert certificate: %w", err)
	}
	return toModel(e), nil
}

// GetByID 查询。
func (r *Repository) GetByID(ctx context.Context, id uuid.UUID) (*Certificate, error) {
	e, err := r.ent.Certificate.Get(ctx, id)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, pkg.ErrNotFound("证书不存在")
		}
		return nil, fmt.Errorf("get certificate: %w", err)
	}
	return toModel(e), nil
}

// GetByHostname 返回某 hostname 最新证书（数据面装载用）。
func (r *Repository) GetByHostname(ctx context.Context, hostname string) (*Certificate, error) {
	e, err := r.ent.Certificate.Query().
		Where(entcert.Hostname(hostname)).
		Order(entcert.ByUpdatedAt()).
		First(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, pkg.ErrNotFound("证书不存在")
		}
		return nil, fmt.Errorf("get certificate by hostname: %w", err)
	}
	return toModel(e), nil
}

// ListByDomainID 返回某域名的证书列表（一般 0..1 生效）。
func (r *Repository) ListByDomainID(ctx context.Context, domainID uuid.UUID) ([]*Certificate, error) {
	es, err := r.ent.Certificate.Query().
		Where(entcert.DomainID(domainID)).
		Order(entcert.ByUpdatedAt()).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("list certificates by domain: %w", err)
	}
	out := make([]*Certificate, 0, len(es))
	for _, e := range es {
		out = append(out, toModel(e))
	}
	return out, nil
}

// List 全量证书（分页）。
func (r *Repository) List(ctx context.Context, limit, offset int) ([]*Certificate, int, error) {
	total, err := r.ent.Certificate.Query().Count(ctx)
	if err != nil {
		return nil, 0, err
	}
	es, err := r.ent.Certificate.Query().
		Order(entcert.ByHostname()).
		Limit(limit).
		Offset(offset).
		All(ctx)
	if err != nil {
		return nil, 0, err
	}
	out := make([]*Certificate, 0, len(es))
	for _, e := range es {
		out = append(out, toModel(e))
	}
	return out, total, nil
}

// All 返回全部证书（缓存装载用）。
func (r *Repository) All(ctx context.Context) ([]*Certificate, error) {
	es, err := r.ent.Certificate.Query().Order(entcert.ByHostname()).All(ctx)
	if err != nil {
		return nil, fmt.Errorf("all certificates: %w", err)
	}
	out := make([]*Certificate, 0, len(es))
	for _, e := range es {
		out = append(out, toModel(e))
	}
	return out, nil
}

// ActiveByExpiryBefore 返回在 deadline 前过期且状态为 active/pending 的证书（临期扫描）。
func (r *Repository) ActiveByExpiryBefore(ctx context.Context, deadline time.Time) ([]*Certificate, error) {
	es, err := r.ent.Certificate.Query().
		Where(
			entcert.ExpiresAtNotNil(),
			entcert.ExpiresAtLT(deadline),
			entcert.StatusIn(string(StatusActive), string(StatusPending)),
		).
		Order(entcert.ByExpiresAt()).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("query expiring certificates: %w", err)
	}
	out := make([]*Certificate, 0, len(es))
	for _, e := range es {
		out = append(out, toModel(e))
	}
	return out, nil
}

// UpdateStatus 更新状态（含 last_error），返回更新后的证书。
func (r *Repository) UpdateStatus(ctx context.Context, id uuid.UUID, status Status, lastErr string) (*Certificate, error) {
	upd := r.ent.Certificate.UpdateOneID(id).
		SetStatus(string(status)).
		SetUpdatedAt(time.Now())
	if lastErr != "" {
		upd = upd.SetLastError(lastErr)
	} else {
		upd = upd.SetLastError("")
	}
	e, err := upd.Save(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, pkg.ErrNotFound("证书不存在")
		}
		return nil, fmt.Errorf("update certificate status: %w", err)
	}
	return toModel(e), nil
}

// UpdateContent 整体替换证书内容（热替换，保留 ID）。
func (r *Repository) UpdateContent(ctx context.Context, id uuid.UUID, in *Certificate) (*Certificate, error) {
	upd := r.ent.Certificate.UpdateOneID(id).
		SetCertificatePem(in.CertificatePEM).
		SetPrivateKeyEncrypted(in.PrivateKeyEncrypted).
		SetHostname(in.Hostname).
		SetSource(string(in.Source)).
		SetStatus(string(in.Status)).
		SetUpdatedAt(time.Now())
	if in.DomainID != nil {
		upd = upd.SetDomainID(*in.DomainID)
	} else {
		upd = upd.ClearDomainID()
	}
	if in.Issuer != "" {
		upd = upd.SetIssuer(in.Issuer)
	}
	if in.SerialNumber != "" {
		upd = upd.SetSerialNumber(in.SerialNumber)
	}
	if in.IssuedAt != nil {
		upd = upd.SetIssuedAt(*in.IssuedAt)
	}
	if in.ExpiresAt != nil {
		upd = upd.SetExpiresAt(*in.ExpiresAt)
	}
	if in.LastError != "" {
		upd = upd.SetLastError(in.LastError)
	} else {
		upd = upd.SetLastError("")
	}
	e, err := upd.Save(ctx)
	if err != nil {
		return nil, fmt.Errorf("update certificate content: %w", err)
	}
	return toModel(e), nil
}

// Delete 删除（Service 层负责清缓存）。
func (r *Repository) Delete(ctx context.Context, id uuid.UUID) error {
	err := r.ent.Certificate.DeleteOneID(id).Exec(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return pkg.ErrNotFound("证书不存在")
		}
		return fmt.Errorf("delete certificate: %w", err)
	}
	return nil
}
