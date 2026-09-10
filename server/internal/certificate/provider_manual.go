package certificate

import "context"

// manualProvider 是"人工上传"来源：证书材料由 Service.Upload/Update 提供，
// provider 本身不支持主动签发/续期，仅把人工来源纳入统一抽象，便于编排层按来源对称分派。
type manualProvider struct {
	repo repoIface
}

func (p *manualProvider) Name() string { return string(SourceManual) }

func (p *manualProvider) Issue(context.Context, IssueRequest) (*Issued, error) {
	return nil, ErrNotSupported
}

func (p *manualProvider) Renew(context.Context, RenewRequest) (*Issued, error) {
	return nil, ErrNotSupported
}

// Revoke 人工来源无 CA 侧撤销；本地删除由 Service.Delete 负责。
func (p *manualProvider) Revoke(context.Context, RevokeRequest) error {
	return ErrNotSupported
}

// Status 直接读本地记录状态。
func (p *manualProvider) Status(ctx context.Context, hostname string) (ProviderStatus, error) {
	rec, err := p.repo.GetByHostname(ctx, hostname)
	if err != nil {
		return ProviderStatus{}, err
	}
	return ProviderStatus{
		State:     string(rec.Status),
		Detail:    rec.Issuer,
		LastError: rec.LastError,
	}, nil
}
