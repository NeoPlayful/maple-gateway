package acme

import "context"

// AccountKey 是某 ACME 目录下的账户凭据。KeyPEM 为**明文**账户私钥，
// 仅在本进程构造 acme.Client 时短暂存在；持久化实现须加密后再落库。
type AccountKey struct {
	DirectoryURL string
	AccountURL   string // CA 侧账户 URL（kid）；空表示尚未注册
	Email        string
	KeyPEM       string
}

// AccountStore 负责账户凭据的持久化（加密由实现负责）。
// 未找到某目录的账户时返回 (nil, nil)，由调用方决定是否新注册。
type AccountStore interface {
	Load(ctx context.Context, directoryURL string) (*AccountKey, error)
	Save(ctx context.Context, k *AccountKey) error
}
