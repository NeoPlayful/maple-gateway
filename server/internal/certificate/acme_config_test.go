package certificate

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/NeoPlayful/maple-gateway/server/internal/certificate/acme"
	"go.uber.org/zap"
)

// TestACMEProvider_DisabledRejectsIssue 验证 enabled=false 时拒绝签发（配置语义：
// 关闭时仅支持手动上传证书），且不触发任何 CA 请求。
func TestACMEProvider_DisabledRejectsIssue(t *testing.T) {
	svc := newServiceWithDeps(newFakeRepo(), NewCache(), roundTripEnc{}, zap.NewNop())
	p := &acmeProvider{svc: svc, challenges: acme.NewChallengeStore(), log: zap.NewNop()}
	p.SetConfig(ACMEConfig{Enabled: false})
	svc.acme = p
	svc.Providers().Register(p)

	_, err := p.Issue(context.Background(), IssueRequest{Hostname: "x.test"})
	if err == nil {
		t.Fatal("disabled acme must reject issuance")
	}
	if _, err := p.Renew(context.Background(), RenewRequest{Hostname: "x.test"}); err == nil {
		t.Fatal("disabled acme must reject renewal")
	}
}

// TestACMEProvider_SetConfigHot 验证 SetConfig 原子替换运行期配置（热更可见）。
func TestACMEProvider_SetConfigHot(t *testing.T) {
	p := &acmeProvider{}
	p.SetConfig(ACMEConfig{Enabled: true, DirectoryURL: "https://a/dir", KeyType: "ec256"})
	if got := p.config(); !got.Enabled || got.DirectoryURL != "https://a/dir" {
		t.Fatalf("initial config = %+v", got)
	}
	p.SetConfig(ACMEConfig{Enabled: false, DirectoryURL: "https://b/dir", KeyType: "rsa2048"})
	got := p.config()
	if got.Enabled || got.DirectoryURL != "https://b/dir" || got.KeyType != "rsa2048" {
		t.Fatalf("after SetConfig = %+v", got)
	}
}

// TestService_ACMEEnabledGate 验证 Service.acmeEnabled 随热更配置变化。
func TestService_ACMEEnabledGate(t *testing.T) {
	svc := newServiceWithDeps(newFakeRepo(), NewCache(), roundTripEnc{}, zap.NewNop())
	if svc.acmeEnabled() {
		t.Error("no acme provider should report disabled")
	}
	p := &acmeProvider{}
	p.SetConfig(ACMEConfig{Enabled: true})
	svc.acme = p
	if !svc.acmeEnabled() {
		t.Error("enabled provider should report true")
	}
	svc.SetACMEConfig(ACMEConfig{Enabled: false})
	if svc.acmeEnabled() {
		t.Error("after disabling, should report false")
	}
}

// TestService_SetRenewConfigHot 验证 RenewNow 走热更参数：
// 将 MaxAttempts 降到 1 后，单次失败即达上限（告警文案含「已达重试上限」）。
func TestService_SetRenewConfigHot(t *testing.T) {
	enc := roundTripEnc{}
	rec := acmeDueCert(t, enc, "hot.test")
	svc := newServiceWithDeps(newFakeRepo(rec), NewCache(), enc, zap.NewNop())
	svc.Providers().Register(&fakeProvider{name: string(SourceACME), err: errors.New("boom")})

	svc.SetRenewConfig(RenewConfig{Before: 30 * 24 * time.Hour, MaxAttempts: 1, BaseBackoff: time.Hour})
	if _, err := svc.RenewNow(context.Background(), rec.ID); err == nil {
		t.Fatal("expected renew error")
	}
	if rec.RenewAttempts != 1 {
		t.Fatalf("attempts = %d, want 1", rec.RenewAttempts)
	}
	// 达上限时错误文案应含「已达重试上限」。
	if !strings.Contains(rec.LastRenewError, "已达重试上限") {
		t.Fatalf("last_renew_error = %q, want 达上限提示", rec.LastRenewError)
	}
}
