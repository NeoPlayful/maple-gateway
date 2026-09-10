package certificate

import (
	"context"
	"time"

	"github.com/NeoPlayful/maple-gateway/server/pkg"
	"github.com/google/uuid"
)

// GetOperation 按 id 查操作进度。
func (s *Service) GetOperation(ctx context.Context, id uuid.UUID) (*CertificateOperation, error) {
	if s.opRepo == nil {
		return nil, pkg.ErrNotFound("操作记录不可用")
	}
	return s.opRepo.GetByID(ctx, id)
}

// LatestOperationByHostname 查某域名最近一次操作：进行中优先，否则返回最近一条记录。
func (s *Service) LatestOperationByHostname(ctx context.Context, hostname string) (*CertificateOperation, error) {
	if s.opRepo == nil {
		return nil, pkg.ErrNotFound("操作记录不可用")
	}
	if op, err := s.opRepo.ActiveByHostname(ctx, hostname); err == nil {
		return op, nil
	}
	return s.opRepo.LatestByHostname(ctx, hostname)
}

// OperationByDomain 按域名 ID 查其 hostname 的最近操作进度。
func (s *Service) OperationByDomain(ctx context.Context, domainID uuid.UUID) (*CertificateOperation, error) {
	if s.opRepo == nil || s.domainRepo == nil {
		return nil, pkg.ErrNotFound("操作记录不可用")
	}
	d, err := s.domainRepo.GetByID(ctx, domainID)
	if err != nil {
		return nil, err
	}
	return s.LatestOperationByHostname(ctx, d.Hostname)
}

// ExpireStaleOperations 把超时未完成的操作标记为 failed（实例重启/进程中断兜底）。
func (s *Service) ExpireStaleOperations(ctx context.Context, olderThan time.Duration) (int, error) {
	if s.opRepo == nil {
		return 0, nil
	}
	return s.opRepo.ExpireStale(ctx, olderThan)
}
