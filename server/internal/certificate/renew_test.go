package certificate

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/NeoPlayful/maple-gateway/server/internal/certificate/acme"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

// fakeProvider 是可注入的 CertificateProvider，用于签发/续期编排测试。
type fakeProvider struct {
	name   string
	issued *Issued
	err    error
	calls  int
}

func (p *fakeProvider) Name() string { return p.name }
func (p *fakeProvider) Issue(_ context.Context, _ IssueRequest) (*Issued, error) {
	p.calls++
	return p.issued, p.err
}
func (p *fakeProvider) Renew(_ context.Context, _ RenewRequest) (*Issued, error) {
	p.calls++
	return p.issued, p.err
}
func (p *fakeProvider) Revoke(_ context.Context, _ RevokeRequest) error { return p.err }
func (p *fakeProvider) Status(_ context.Context, _ string) (ProviderStatus, error) {
	return ProviderStatus{}, nil
}

// acmeDueCert 构造一条临期 acme 证书（5 天后到期），私钥已加密。
func acmeDueCert(t *testing.T, enc encIface, host string) *Certificate {
	t.Helper()
	certPEM, keyPEM, leaf := genCert(t, []string{host}, time.Now().Add(-24*time.Hour), time.Now().Add(5*24*time.Hour))
	encKey, err := enc.Encrypt([]byte(keyPEM))
	if err != nil {
		t.Fatal(err)
	}
	return &Certificate{
		ID:                  uuid.New(),
		Hostname:            host,
		Source:              SourceACME,
		Status:              StatusActive,
		CertificatePEM:      certPEM,
		PrivateKeyEncrypted: encKey,
		ExpiresAt:           &leaf.NotAfter,
	}
}

func TestRenewOnceSuccessResetsAndRefreshes(t *testing.T) {
	enc := roundTripEnc{}
	rec := acmeDueCert(t, enc, "renew.test")
	svc := newServiceWithDeps(newFakeRepo(rec), NewCache(), enc, zap.NewNop())

	newPEM, newKey, _ := genCert(t, []string{"renew.test"}, time.Now(), time.Now().Add(90*24*time.Hour))
	p := &fakeProvider{name: string(SourceACME), issued: &Issued{
		CertificatePEM: newPEM, PrivateKeyPEM: newKey, NotAfter: time.Now().Add(90 * 24 * time.Hour),
	}}
	svc.Providers().Register(p)

	n, err := svc.RenewOnce(context.Background(), RenewConfig{Before: 30 * 24 * time.Hour, MaxAttempts: 5}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("want 1 renewed, got %d", n)
	}
	if p.calls != 1 {
		t.Fatalf("provider renew calls = %d, want 1", p.calls)
	}
	if rec.RenewAttempts != 0 {
		t.Errorf("renew_attempts should reset to 0, got %d", rec.RenewAttempts)
	}
	if rec.LastRenewedAt == nil {
		t.Error("last_renewed_at should be set")
	}
	if rec.NextRenewAt == nil || !rec.NextRenewAt.After(time.Now()) {
		t.Error("next_renew_at should be in the future")
	}
	if svc.Cache().Get("renew.test") == nil {
		t.Error("cache should be refreshed with new certificate")
	}
}

func TestRenewOnceFailureBacksOff(t *testing.T) {
	enc := roundTripEnc{}
	rec := acmeDueCert(t, enc, "fail.test")
	svc := newServiceWithDeps(newFakeRepo(rec), NewCache(), enc, zap.NewNop())
	p := &fakeProvider{name: string(SourceACME), err: errors.New("acme: transient error")}
	svc.Providers().Register(p)

	n, err := svc.RenewOnce(context.Background(), RenewConfig{
		Before: 30 * 24 * time.Hour, MaxAttempts: 5, BaseBackoff: time.Hour,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("want 0 renewed on failure, got %d", n)
	}
	if rec.RenewAttempts != 1 {
		t.Errorf("renew_attempts = %d, want 1", rec.RenewAttempts)
	}
	if rec.LastRenewError == "" {
		t.Error("last_renew_error should be recorded")
	}
	if rec.NextRenewAt == nil || !rec.NextRenewAt.After(time.Now()) {
		t.Error("next_renew_at should be advanced (backoff)")
	}
	// 失败不得刷新缓存（保留旧证继续服务）。
	if svc.Cache().Get("fail.test") != nil {
		t.Error("cache should keep serving old certificate on renew failure")
	}
}

func TestRenewOnceRespectsCanRunGate(t *testing.T) {
	enc := roundTripEnc{}
	rec := acmeDueCert(t, enc, "gate.test")
	svc := newServiceWithDeps(newFakeRepo(rec), NewCache(), enc, zap.NewNop())
	p := &fakeProvider{name: string(SourceACME), issued: &Issued{}}
	svc.Providers().Register(p)

	// 非 Leader：本轮不执行，provider 不被调用。
	n, err := svc.RenewOnce(context.Background(), RenewConfig{Before: 30 * 24 * time.Hour}, func() bool { return false })
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 || p.calls != 0 {
		t.Fatalf("gate=false should skip renewal: n=%d calls=%d", n, p.calls)
	}
}

func TestRenewOnceSkipsManualSource(t *testing.T) {
	enc := roundTripEnc{}
	// manual 来源不在 DueForRenewal 候选内（仅 acme），故不会被续期。
	certPEM, keyPEM, leaf := genCert(t, []string{"manual.test"}, time.Now().Add(-time.Hour), time.Now().Add(time.Hour))
	encKey, _ := enc.Encrypt([]byte(keyPEM))
	rec := &Certificate{ID: uuid.New(), Hostname: "manual.test", Source: SourceManual, Status: StatusActive,
		CertificatePEM: certPEM, PrivateKeyEncrypted: encKey, ExpiresAt: &leaf.NotAfter}
	svc := newServiceWithDeps(newFakeRepo(rec), NewCache(), enc, zap.NewNop())

	n, err := svc.RenewOnce(context.Background(), RenewConfig{Before: 30 * 24 * time.Hour}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("manual-source cert must not be auto-renewed, got n=%d", n)
	}
}

func TestBackoffDurationRateLimitFloor(t *testing.T) {
	cfg := RenewConfig{BaseBackoff: time.Hour, RateBackoff: 6 * time.Hour}
	// 限流时不低于 RateBackoff。
	if d := backoffDuration(cfg, 1, errors.Join(acme.ErrRateLimited)); d < 6*time.Hour {
		t.Errorf("rate-limited backoff = %v, want >= 6h", d)
	}
	// 指数增长封顶 24h。
	if d := backoffDuration(cfg, 10, errors.New("x")); d > 24*time.Hour {
		t.Errorf("backoff = %v, want <= 24h", d)
	}
}
