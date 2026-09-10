package acme

import (
	"errors"
	"strings"

	"golang.org/x/crypto/acme"
)

// 分类错误：续期引擎据类型决定退避策略（限流加长、其余按指数退避、账户无效停重试）。
var (
	// ErrRateLimited CA 限流（429 / rateLimited）。
	ErrRateLimited = errors.New("acme: rate limited by CA")
	// ErrAccountInvalid 账户无效/密钥失配，停止自动签发并告警。
	ErrAccountInvalid = errors.New("acme: account invalid")
	// ErrChallengeFailed 域名验证失败（http-01 未通过）。
	ErrChallengeFailed = errors.New("acme: challenge failed")
	// ErrTransient 瞬时错误（网络/DNS/5xx），可短期重试。
	ErrTransient = errors.New("acme: transient error")
)

// classify 把 acme 库错误归类为上述分类错误（保留原始错误链）。
func classify(err error) error {
	if err == nil {
		return nil
	}
	var aerr *acme.Error
	if errors.As(err, &aerr) {
		// acme.Error.ProblemType 形如 "urn:ietf:params:acme:error:rateLimited"。
		pt := string(aerr.ProblemType)
		switch {
		case strings.Contains(pt, "rateLimited") || aerr.StatusCode == 429:
			return errors.Join(ErrRateLimited, err)
		case strings.Contains(pt, "accountDoesNotExist"),
			strings.Contains(pt, "unauthorized"),
			strings.Contains(pt, "invalidContact"):
			return errors.Join(ErrAccountInvalid, err)
		case strings.Contains(pt, "dns"), strings.Contains(pt, "connection"),
			strings.Contains(pt, "tls"), strings.Contains(pt, "unauthorized"):
			return errors.Join(ErrChallengeFailed, err)
		default:
			if aerr.StatusCode >= 500 {
				return errors.Join(ErrTransient, err)
			}
			return err
		}
	}
	return errors.Join(ErrTransient, err)
}

// IsRateLimited / IsAccountInvalid / IsChallengeFailed / IsTransient 便于续期引擎分支。
func IsRateLimited(err error) bool     { return errors.Is(err, ErrRateLimited) }
func IsAccountInvalid(err error) bool  { return errors.Is(err, ErrAccountInvalid) }
func IsChallengeFailed(err error) bool { return errors.Is(err, ErrChallengeFailed) }
func IsTransient(err error) bool       { return errors.Is(err, ErrTransient) }
