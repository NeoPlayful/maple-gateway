package certificate

import (
	"context"
	"fmt"
	"time"

	"github.com/NeoPlayful/maple-gateway/server/ent"
	entop "github.com/NeoPlayful/maple-gateway/server/ent/certificateoperation"
	"github.com/NeoPlayful/maple-gateway/server/pkg"
	"github.com/google/uuid"
)

// OperationRepository 是 CertificateOperation 数据访问层（基于 Ent）。
type OperationRepository struct {
	ent *ent.Client
}

// NewOperationRepository 构造。
func NewOperationRepository(client *ent.Client) *OperationRepository {
	return &OperationRepository{ent: client}
}

func toOperationModel(e *ent.CertificateOperation) *CertificateOperation {
	return &CertificateOperation{
		ID:         e.ID,
		Hostname:   e.Hostname,
		DomainID:   e.DomainID,
		Action:     e.Action,
		Status:     OperationStatus(e.Status),
		Message:    e.Message,
		Error:      e.Error,
		StartedAt:  e.StartedAt,
		UpdatedAt:  e.UpdatedAt,
		FinishedAt: e.FinishedAt,
	}
}

// Create 插入一条进行中的操作记录（status 默认 queued）。
// hostname 上存在"进行中唯一"部分索引：同域名已有进行中操作时返回冲突。
func (r *OperationRepository) Create(ctx context.Context, in *CertificateOperation) (*CertificateOperation, error) {
	now := time.Now()
	cr := r.ent.CertificateOperation.Create().
		SetHostname(in.Hostname).
		SetAction(in.Action).
		SetStatus(string(in.Status)).
		SetStartedAt(now).
		SetUpdatedAt(now)
	if in.DomainID != nil {
		cr = cr.SetDomainID(*in.DomainID)
	}
	if in.Message != "" {
		cr = cr.SetMessage(in.Message)
	}
	e, err := cr.Save(ctx)
	if err != nil {
		if pkg.IsUniqueViolation(err) {
			return nil, pkg.ErrConflict("该域名已有进行中的签发操作")
		}
		return nil, fmt.Errorf("insert certificate operation: %w", err)
	}
	return toOperationModel(e), nil
}

// UpdateStep 推进阶段（非终态）：更新 status/message 并刷新 updated_at。
func (r *OperationRepository) UpdateStep(ctx context.Context, id uuid.UUID, status OperationStatus, message string) error {
	err := r.ent.CertificateOperation.UpdateOneID(id).
		SetStatus(string(status)).
		SetMessage(message).
		SetUpdatedAt(time.Now()).
		Exec(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return pkg.ErrNotFound("操作记录不存在")
		}
		return fmt.Errorf("update certificate operation step: %w", err)
	}
	return nil
}

// Finish 写入终态（active / failed），记录完成时间与失败原因。
func (r *OperationRepository) Finish(ctx context.Context, id uuid.UUID, status OperationStatus, message, errMsg string) error {
	now := time.Now()
	upd := r.ent.CertificateOperation.UpdateOneID(id).
		SetStatus(string(status)).
		SetMessage(message).
		SetUpdatedAt(now).
		SetFinishedAt(now)
	if errMsg != "" {
		upd = upd.SetError(errMsg)
	}
	if err := upd.Exec(ctx); err != nil {
		if ent.IsNotFound(err) {
			return pkg.ErrNotFound("操作记录不存在")
		}
		return fmt.Errorf("finish certificate operation: %w", err)
	}
	return nil
}

// GetByID 查询。
func (r *OperationRepository) GetByID(ctx context.Context, id uuid.UUID) (*CertificateOperation, error) {
	e, err := r.ent.CertificateOperation.Get(ctx, id)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, pkg.ErrNotFound("操作记录不存在")
		}
		return nil, fmt.Errorf("get certificate operation: %w", err)
	}
	return toOperationModel(e), nil
}

// ActiveByHostname 返回某 hostname 当前进行中（非终态）的操作；无则 NotFound。
func (r *OperationRepository) ActiveByHostname(ctx context.Context, hostname string) (*CertificateOperation, error) {
	e, err := r.ent.CertificateOperation.Query().
		Where(
			entop.Hostname(hostname),
			entop.StatusNotIn(string(OpActive), string(OpFailed)),
		).
		Order(entop.ByUpdatedAt()).
		First(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, pkg.ErrNotFound("无进行中的操作")
		}
		return nil, fmt.Errorf("get active certificate operation: %w", err)
	}
	return toOperationModel(e), nil
}

// LatestByHostname 返回某 hostname 最近一次操作（含终态），供展示历史进度。
func (r *OperationRepository) LatestByHostname(ctx context.Context, hostname string) (*CertificateOperation, error) {
	e, err := r.ent.CertificateOperation.Query().
		Where(entop.Hostname(hostname)).
		Order(entop.ByUpdatedAt()).
		First(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, pkg.ErrNotFound("无操作记录")
		}
		return nil, fmt.Errorf("get latest certificate operation: %w", err)
	}
	return toOperationModel(e), nil
}

// ExpireStale 把超过 olderThan 仍未完成的进行中操作标记为 failed（实例重启/进程崩溃兜底）。
// 返回被标记的条数。
func (r *OperationRepository) ExpireStale(ctx context.Context, olderThan time.Duration) (int, error) {
	deadline := time.Now().Add(-olderThan)
	now := time.Now()
	n, err := r.ent.CertificateOperation.Update().
		Where(
			entop.StatusNotIn(string(OpActive), string(OpFailed)),
			entop.UpdatedAtLT(deadline),
		).
		SetStatus(string(OpFailed)).
		SetError("操作超时（进程中断或长时间无进展）").
		SetUpdatedAt(now).
		SetFinishedAt(now).
		Save(ctx)
	if err != nil {
		return 0, fmt.Errorf("expire stale certificate operations: %w", err)
	}
	return n, nil
}
