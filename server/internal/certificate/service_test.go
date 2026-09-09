package certificate

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

// fakeRepo 是 repoIface 的内存实现，覆盖 Service 用到的查询/更新。
type fakeRepo struct {
	mu   sync.Mutex
	all  []*Certificate
	byID map[uuid.UUID]*Certificate
}

func newFakeRepo(recs ...*Certificate) *fakeRepo {
	f := &fakeRepo{byID: make(map[uuid.UUID]*Certificate)}
	for _, r := range recs {
		if r.ID == uuid.Nil {
			r.ID = uuid.New()
		}
		f.all = append(f.all, r)
		f.byID[r.ID] = r
	}
	return f
}

// byHost 定位 host 对应的记录（测试内使用，需持有锁）。
func (f *fakeRepo) byHost(host string) *Certificate {
	for _, r := range f.all {
		if r.Hostname == host {
			return r
		}
	}
	return nil
}

func (f *fakeRepo) All(context.Context) ([]*Certificate, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]*Certificate, len(f.all))
	copy(out, f.all)
	return out, nil
}

func (f *fakeRepo) GetByHostname(_ context.Context, hostname string) (*Certificate, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if r := f.byHost(hostname); r != nil {
		return r, nil
	}
	return nil, errors.New("not found")
}

func (f *fakeRepo) UpdateContent(_ context.Context, id uuid.UUID, in *Certificate) (*Certificate, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	r, ok := f.byID[id]
	if !ok {
		return nil, errors.New("not found")
	}
	r.CertificatePEM = in.CertificatePEM
	r.PrivateKeyEncrypted = in.PrivateKeyEncrypted
	r.Status = in.Status
	return r, nil
}

func (f *fakeRepo) Delete(_ context.Context, id uuid.UUID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.byID[id]; !ok {
		return errors.New("not found")
	}
	delete(f.byID, id)
	for i, r := range f.all {
		if r.ID == id {
			f.all = append(f.all[:i], f.all[i+1:]...)
			break
		}
	}
	return nil
}

// ActiveByExpiryBefore 复刻真实语义：仅 active/pending 且在 deadline 前到期的记录。
func (f *fakeRepo) ActiveByExpiryBefore(_ context.Context, deadline time.Time) ([]*Certificate, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []*Certificate
	for _, r := range f.all {
		if r.Status != StatusActive && r.Status != StatusPending {
			continue
		}
		if r.ExpiresAt != nil && r.ExpiresAt.Before(deadline) {
			out = append(out, r)
		}
	}
	return out, nil
}

func (f *fakeRepo) UpdateStatus(_ context.Context, id uuid.UUID, status Status, lastErr string) (*Certificate, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	r, ok := f.byID[id]
	if !ok {
		return nil, errors.New("not found")
	}
	r.Status = status
	r.LastError = lastErr
	return r, nil
}

func (f *fakeRepo) GetByID(_ context.Context, id uuid.UUID) (*Certificate, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	r, ok := f.byID[id]
	if !ok {
		return nil, errors.New("not found")
	}
	return r, nil
}

func (f *fakeRepo) Create(_ context.Context, in *Certificate) (*Certificate, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	cp := *in
	cp.ID = uuid.New()
	f.byID[cp.ID] = &cp
	f.all = append(f.all, &cp)
	return &cp, nil
}

// List 不参与 Service 准入测试，返回空以满足接口。
func (f *fakeRepo) List(_ context.Context, _ int, _ int) ([]*Certificate, int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return nil, 0, nil
}

// mustCert 构造覆盖 host 的自签证书模型：私钥已用 enc 加密（模拟落库后形态）。
func mustCert(t *testing.T, host string, status Status, notBefore, notAfter time.Time, enc encIface) *Certificate {
	t.Helper()
	certPEM, keyPEM, leaf := genCert(t, []string{host}, notBefore, notAfter)
	encKey, err := enc.Encrypt([]byte(keyPEM))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	return &Certificate{
		ID:                  uuid.New(),
		Hostname:            host,
		Source:              SourceManual,
		Status:              status,
		CertificatePEM:      certPEM,
		PrivateKeyEncrypted: encKey,
		IssuedAt:            &now,
		ExpiresAt:           &leaf.NotAfter,
	}
}

// roundTripEnc 是测试用无损加解密（往返一致），绕过 MAPLE_CERT_ENC_KEY 依赖。
type roundTripEnc struct{}

func (roundTripEnc) Encrypt(plaintext []byte) (string, error) { return string(plaintext), nil }
func (roundTripEnc) Decrypt(encoded string) ([]byte, error)    { return []byte(encoded), nil }

func mustEnc(t *testing.T) encIface { return roundTripEnc{} }

func mustService(recs ...*Certificate) *Service {
	return newServiceWithDeps(newFakeRepo(recs...), NewCache(), roundTripEnc{}, zap.NewNop())
}

// TestReloadAllKeepsEveryServiceableStatus: 5s 全量对账不因 status 把临期/过期证书踢出缓存。
func TestReloadAllKeepsEveryServiceableStatus(t *testing.T) {
	enc := roundTripEnc{}
	now := time.Now()
	recs := []*Certificate{
		mustCert(t, "active.test", StatusActive, now.Add(-time.Hour), now.Add(24*time.Hour), enc),
		mustCert(t, "pending.test", StatusPending, now.Add(-time.Hour), now.Add(24*time.Hour), enc),
		mustCert(t, "expiring.test", StatusExpiring, now.Add(-48*time.Hour), now.Add(3*time.Hour), enc),
		mustCert(t, "expired.test", StatusExpired, now.Add(-96*time.Hour), now.Add(-24*time.Hour), enc),
	}
	svc := mustService(recs...)

	if err := svc.ReloadAll(context.Background()); err != nil {
		t.Fatalf("ReloadAll: %v", err)
	}
	want := map[string]Status{
		"active.test":  StatusActive,
		"pending.test": StatusPending,
		"expiring.test": StatusExpiring,
		"expired.test":  StatusExpired,
	}
	for host := range want {
		if svc.cache.Get(host) == nil {
			t.Errorf("host %s (status %s) must stay in cache after ReloadAll", host, want[host])
		}
	}
}

// TestReloadAllSkipsUnloadableOnly: 只有内容无法装载（密文损坏/密钥不匹配）的证书被剔除。
func TestReloadAllSkipsUnloadableOnly(t *testing.T) {
	enc := roundTripEnc{}
	now := time.Now()
	good := mustCert(t, "good.test", StatusExpired, now.Add(-96*time.Hour), now.Add(-24*time.Hour), enc) // 过期但仍可装载
	bad := mustCert(t, "bad.test", StatusActive, now.Add(-time.Hour), now.Add(24*time.Hour), enc)
	bad.PrivateKeyEncrypted = "corrupt-blob" // 无法解密
	wrong := mustCert(t, "wrong.test", StatusActive, now.Add(-time.Hour), now.Add(24*time.Hour), enc)
	wrong.PrivateKeyEncrypted = "garbage-key" // 解密出非法 PEM

	svc := mustService(good, bad, wrong)
	if err := svc.ReloadAll(context.Background()); err != nil {
		t.Fatalf("ReloadAll: %v", err)
	}
	if svc.cache.Get("good.test") == nil {
		t.Error("expired-but-loadable cert should stay cached")
	}
	if svc.cache.Get("bad.test") != nil {
		t.Error("cert with undecryptable private key must be dropped")
	}
	if svc.cache.Get("wrong.test") != nil {
		t.Error("cert that cannot assemble a tls pair must be dropped")
	}
}

// TestReloadIntoCacheAcceptsExpired: 单条热加载（手动 reload）不再按 status 拒绝过期证书。
func TestReloadIntoCacheAcceptsExpired(t *testing.T) {
	enc := roundTripEnc{}
	now := time.Now()
	rec := mustCert(t, "expired.test", StatusExpired, now.Add(-96*time.Hour), now.Add(-24*time.Hour), enc)

	svc := mustService()
	if err := svc.reloadIntoCache(rec); err != nil {
		t.Fatalf("reloadIntoCache should accept expired cert: %v", err)
	}
	if svc.cache.Get("expired.test") == nil {
		t.Error("expired cert should be cached and keep serving")
	}
}

// TestScanExpiringMarksButNeverEvicts: 扫描只更新状态（告警维度），不摘缓存、不中断服务。
func TestScanExpiringMarksButNeverEvicts(t *testing.T) {
	enc := roundTripEnc{}
	now := time.Now()
	expired := mustCert(t, "expired.test", StatusActive, now.Add(-96*time.Hour), now.Add(-time.Hour), enc)
	expiring := mustCert(t, "expiring.test", StatusActive, now.Add(-48*time.Hour), now.Add(2*time.Hour), enc)
	repo := newFakeRepo(expired, expiring)
	svc := newServiceWithDeps(repo, NewCache(), enc, zap.NewNop())

	// 预装缓存：两者都已在服务中。
	if err := svc.ReloadAll(context.Background()); err != nil {
		t.Fatalf("ReloadAll: %v", err)
	}
	// 把两者真实过期时间都挪到过去，确保扫描走"已过期→置 expired"分支。
	repo.mu.Lock()
	for _, r := range repo.all {
		past := now.Add(-time.Minute)
		r.ExpiresAt = &past
	}
	repo.mu.Unlock()

	n, err := svc.ScanExpiring(context.Background(), 30*24*time.Hour)
	if err != nil {
		t.Fatalf("ScanExpiring: %v", err)
	}
	if n != 2 {
		t.Fatalf("expected 2 certs updated to expired, got %d", n)
	}
	for _, host := range []string{"expired.test", "expiring.test"} {
		if r := repo.byHost(host); r.Status != StatusExpired {
			t.Errorf("host %s status should be expired, got %s", host, r.Status)
		}
		if svc.cache.Get(host) == nil {
			t.Errorf("host %s must remain in cache after ScanExpiring (service keeps running)", host)
		}
	}
}
