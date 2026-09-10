package certificate

import (
	"context"
	"fmt"
	"time"

	"github.com/NeoPlayful/maple-gateway/server/ent"
	entacct "github.com/NeoPlayful/maple-gateway/server/ent/acmeaccount"
	"github.com/NeoPlayful/maple-gateway/server/internal/certificate/acme"
	"github.com/NeoPlayful/maple-gateway/server/pkg"
	"go.uber.org/zap"
)

// acmeAccountRepo 用 ent + certenc 持久化 ACME 账户：账户私钥加密后落库，读取时解密。
// 实现 acme.AccountStore。明文密钥只在本进程内存短存，绝不落库/回显/日志。
type acmeAccountRepo struct {
	ent *ent.Client
	enc encIface
	log *zap.Logger
}

// Load 按 directory_url 取账户；未找到返回 (nil, nil) 由调用方决定新注册。
func (r *acmeAccountRepo) Load(ctx context.Context, directoryURL string) (*acme.AccountKey, error) {
	e, err := r.ent.ACMEAccount.Query().
		Where(entacct.DirectoryURL(directoryURL)).
		Order(entacct.ByUpdatedAt()).
		First(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("load acme account: %w", err)
	}
	keyPEM, err := r.enc.Decrypt(e.KeyEncrypted)
	if err != nil {
		return nil, fmt.Errorf("decrypt acme account key: %w", err)
	}
	return &acme.AccountKey{
		DirectoryURL: e.DirectoryURL,
		AccountURL:   e.AccountURL,
		Email:        e.Email,
		KeyPEM:       string(keyPEM),
	}, nil
}

// Save 以 directory_url 为键 upsert 账户（密文私钥落库）。
func (r *acmeAccountRepo) Save(ctx context.Context, k *acme.AccountKey) error {
	encKey, err := r.enc.Encrypt([]byte(k.KeyPEM))
	if err != nil {
		return pkg.ErrSystem("账户密钥加密失败")
	}
	now := time.Now()
	existing, qerr := r.ent.ACMEAccount.Query().
		Where(entacct.DirectoryURL(k.DirectoryURL)).
		First(ctx)
	switch {
	case qerr == nil:
		_, err = r.ent.ACMEAccount.UpdateOneID(existing.ID).
			SetAccountURL(k.AccountURL).
			SetEmail(k.Email).
			SetKeyEncrypted(encKey).
			SetStatus("active").
			SetLastError("").
			SetUpdatedAt(now).
			Save(ctx)
	case ent.IsNotFound(qerr):
		_, err = r.ent.ACMEAccount.Create().
			SetDirectoryURL(k.DirectoryURL).
			SetAccountURL(k.AccountURL).
			SetEmail(k.Email).
			SetKeyEncrypted(encKey).
			SetCreatedAt(now).
			SetUpdatedAt(now).
			Save(ctx)
	default:
		return fmt.Errorf("query acme account: %w", qerr)
	}
	if err != nil {
		return fmt.Errorf("save acme account: %w", err)
	}
	return nil
}

// MarkError 记录账户异常（不覆盖密钥，停止自动签发等待人工介入）。
func (r *acmeAccountRepo) MarkError(ctx context.Context, directoryURL, msg string) {
	if _, err := r.ent.ACMEAccount.Update().
		Where(entacct.DirectoryURL(directoryURL)).
		SetStatus("error").
		SetLastError(msg).
		SetUpdatedAt(time.Now()).
		Save(ctx); err != nil && r.log != nil {
		r.log.Warn("mark acme account error failed",
			zap.String("directory", directoryURL), zap.Error(err))
	}
}

var _ acme.AccountStore = (*acmeAccountRepo)(nil)
