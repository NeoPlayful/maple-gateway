package certificate

import (
	"context"
	"encoding/base64"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/NeoPlayful/maple-gateway/server/internal/certificate/certenc"
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
	// 复刻真实 UpdateContent 语义：整体覆盖内容与派生字段。
	r.CertificatePEM = in.CertificatePEM
	r.PrivateKeyEncrypted = in.PrivateKeyEncrypted
	r.Status = in.Status
	r.Hostname = in.Hostname
	r.DomainID = in.DomainID
	r.Source = in.Source
	r.Issuer = in.Issuer
	r.SerialNumber = in.SerialNumber
	r.IssuedAt = in.IssuedAt
	r.ExpiresAt = in.ExpiresAt
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

// TestServiceUpdateReplacesMaterialKeepingHostname: 按 id 更换材料后 hostname 不变、
// 派生字段刷新、缓存热替换；domain 绑定缺省保持原值。
func TestServiceUpdateReplacesMaterialKeepingHostname(t *testing.T) {
	enc := roundTripEnc{}
	now := time.Now()
	old := mustCert(t, "keep.test", StatusActive, now.Add(-24*time.Hour), now.Add(24*time.Hour), enc)
	oldDomain := uuid.New()
	old.DomainID = &oldDomain
	repo := newFakeRepo(old)
	svc := newServiceWithDeps(repo, NewCache(), enc, zap.NewNop())

	newCertPEM, newKeyPEM, newLeaf := genCert(t, []string{"keep.test"}, now, now.Add(365*24*time.Hour))
	got, err := svc.Update(context.Background(), old.ID, UpdateRequest{
		CertificatePEM: newCertPEM,
		PrivateKeyPEM:  newKeyPEM,
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if got.ID != old.ID {
		t.Errorf("record id must be preserved, got %s want %s", got.ID, old.ID)
	}
	if got.Hostname != "keep.test" {
		t.Errorf("hostname must stay unchanged, got %q", got.Hostname)
	}
	if got.CertificatePEM != newCertPEM {
		t.Error("certificate content should be replaced")
	}
	if got.ExpiresAt == nil || !got.ExpiresAt.Equal(newLeaf.NotAfter) {
		t.Errorf("expiry should be refreshed to %v, got %v", newLeaf.NotAfter, got.ExpiresAt)
	}
	if got.DomainID == nil || *got.DomainID != oldDomain {
		t.Errorf("domain binding should be preserved by default, got %v", got.DomainID)
	}
	if svc.cache.Get("keep.test") == nil {
		t.Error("updated cert must be hot-reloaded into cache")
	}
}

// TestServiceUpdateDomainBindingThreeState: domain 绑定三态——换绑 / 解绑 / 保持。
func TestServiceUpdateDomainBindingThreeState(t *testing.T) {
	enc := roundTripEnc{}
	now := time.Now()
	certPEM, keyPEM, _ := genCert(t, []string{"bind.test"}, now, now.Add(24*time.Hour))

	cases := []struct {
		name   string
		mutate func(in *UpdateRequest, newDomain uuid.UUID)
		check  func(t *testing.T, got *Certificate, orig, newDomain uuid.UUID)
	}{
		{
			name:   "rebind",
			mutate: func(in *UpdateRequest, d uuid.UUID) { in.DomainID = &d },
			check: func(t *testing.T, got *Certificate, _, d uuid.UUID) {
				if got.DomainID == nil || *got.DomainID != d {
					t.Errorf("expected rebind to %s, got %v", d, got.DomainID)
				}
			},
		},
		{
			name:   "unbind",
			mutate: func(in *UpdateRequest, _ uuid.UUID) { in.ClearDomainID = true },
			check: func(t *testing.T, got *Certificate, _, _ uuid.UUID) {
				if got.DomainID != nil {
					t.Errorf("expected unbind (nil), got %v", got.DomainID)
				}
			},
		},
		{
			name:   "keep",
			mutate: func(*UpdateRequest, uuid.UUID) {},
			check: func(t *testing.T, got *Certificate, orig, _ uuid.UUID) {
				if got.DomainID == nil || *got.DomainID != orig {
					t.Errorf("expected keep original %s, got %v", orig, got.DomainID)
				}
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			orig := uuid.New()
			old := mustCert(t, "bind.test", StatusActive, now.Add(-time.Hour), now.Add(24*time.Hour), enc)
			old.DomainID = &orig
			repo := newFakeRepo(old)
			svc := newServiceWithDeps(repo, NewCache(), enc, zap.NewNop())

			newDomain := uuid.New()
			var in UpdateRequest
			in.CertificatePEM = certPEM
			in.PrivateKeyPEM = keyPEM
			tc.mutate(&in, newDomain)

			got, err := svc.Update(context.Background(), old.ID, in)
			if err != nil {
				t.Fatalf("Update: %v", err)
			}
			tc.check(t, got, orig, newDomain)
		})
	}
}

// TestServiceUpdateRejectsMismatchedPEM: 证书/私钥不匹配时不落库、不改缓存。
func TestServiceUpdateRejectsMismatchedPEM(t *testing.T) {
	enc := roundTripEnc{}
	now := time.Now()
	old := mustCert(t, "bad.test", StatusActive, now.Add(-time.Hour), now.Add(24*time.Hour), enc)
	repo := newFakeRepo(old)
	svc := newServiceWithDeps(repo, NewCache(), enc, zap.NewNop())

	// certPEM 来自一张证书，私钥来自另一张（不同密钥对）→ 不匹配。
	certPEM, _, _ := genCert(t, []string{"bad.test"}, now, now.Add(24*time.Hour))
	_, mismatchedKey, _ := genCert(t, []string{"bad.test"}, now, now.Add(24*time.Hour))

	if _, err := svc.Update(context.Background(), old.ID, UpdateRequest{
		CertificatePEM: certPEM,
		PrivateKeyPEM:  mismatchedKey,
	}); err == nil {
		t.Fatal("mismatched cert/key must be rejected")
	}
}

// TestNewServiceUsesConfiguredEncKey: WithEncKey 传入合法 base64 密钥时，
// NewService 用真实 AES 加密而非回退（密文能被同一把解密、不等于明文）。
func TestNewServiceUsesConfiguredEncKey(t *testing.T) {
	raw := make([]byte, 32)
	for i := range raw {
		raw[i] = byte(i)
	}
	key := base64.StdEncoding.EncodeToString(raw)

	svc, err := NewService(newFakeRepo(), NewCache(), zap.NewNop(), WithEncKey(key))
	if err != nil {
		t.Fatalf("NewService with configured key: %v", err)
	}
	plain := []byte("-----BEGIN PRIVATE KEY-----\n...\n-----END PRIVATE KEY-----")
	ct, err := svc.enc.Encrypt(plain)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	if string(ct) == string(plain) {
		t.Fatal("configured key must actually encrypt, not round-trip as plaintext")
	}
	// 用独立构造的同一把 key 解密，证明走的是真实 AES 且可逆。
	box, err := certenc.NewFromEnv(key)
	if err != nil {
		t.Fatalf("new certenc box: %v", err)
	}
	got, err := box.Decrypt(ct)
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}
	if string(got) != string(plain) {
		t.Fatal("round trip mismatch")
	}
}

// TestNewServiceWithoutKeyFails: 无 key（config 与 env 均空）时 NewService 返回 ErrNoKey。
func TestNewServiceWithoutKeyFails(t *testing.T) {
	t.Setenv("MAPLE_CERT_ENC_KEY", "")
	if _, err := NewService(newFakeRepo(), NewCache(), zap.NewNop()); err == nil {
		t.Fatal("NewService without any key must fail (ErrNoKey)")
	}
}
