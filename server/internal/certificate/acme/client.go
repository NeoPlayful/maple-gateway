package acme

import (
	"context"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"net/http"
	"strings"
	"time"

	"go.uber.org/zap"
	"golang.org/x/crypto/acme"
)

// challengeTTL 是 http-01 挑战应答在 store 中的存活时间；CA 通常在秒级完成验证。
const challengeTTL = 15 * time.Minute

// Config 构造 ACME 客户端所需依赖。
type Config struct {
	DirectoryURL string
	Email        string
	Challenge    string // http-01（默认，本期唯一实现）；tls-alpn-01 预留
	KeyType      KeyType
	AccountStore AccountStore
	Challenges   *ChallengeStore
	Log          *zap.Logger
	HTTPClient   *http.Client // 可空；用于测试注入或代理
}

// Client 是 ACME 自动签发/续期客户端。无状态，每次操作按需确保账户。
type Client struct {
	cfg Config
}

// NewClient 构造客户端。
func NewClient(cfg Config) *Client { return &Client{cfg: cfg} }

// Issued 是一次签发/续期的产出材料（明文私钥仅内存流转）。
type Issued struct {
	CertificatePEM string
	PrivateKeyPEM  string
	NotAfter       time.Time
	Issuer         string
	SerialNumber   string
	OrderURL       string
	Challenge      string
}

// EnsureAccount 加载或注册 ACME 账户，返回绑定了 KID 的底层客户端。
func (c *Client) EnsureAccount(ctx context.Context) (*acme.Client, *AccountKey, error) {
	ak, err := c.cfg.AccountStore.Load(ctx, c.cfg.DirectoryURL)
	if err != nil {
		return nil, nil, err
	}
	if ak == nil {
		signer, gerr := generateKey(KeyEC256)
		if gerr != nil {
			return nil, nil, gerr
		}
		keyPEM, merr := marshalKeyPEM(signer)
		if merr != nil {
			return nil, nil, merr
		}
		ak = &AccountKey{DirectoryURL: c.cfg.DirectoryURL, Email: c.cfg.Email, KeyPEM: keyPEM}
	}
	signer, err := parseKeyPEM(ak.KeyPEM)
	if err != nil {
		return nil, nil, err
	}
	cl := &acme.Client{Key: signer, DirectoryURL: c.cfg.DirectoryURL}
	if c.cfg.HTTPClient != nil {
		cl.HTTPClient = c.cfg.HTTPClient
	}
	if ak.AccountURL == "" {
		acct := &acme.Account{}
		if c.cfg.Email != "" {
			acct.Contact = []string{"mailto:" + c.cfg.Email}
		}
		a, rerr := cl.Register(ctx, acct, acme.AcceptTOS)
		if rerr != nil {
			return nil, nil, classify(rerr)
		}
		ak.AccountURL = a.URI
		if serr := c.cfg.AccountStore.Save(ctx, ak); serr != nil {
			return nil, nil, serr
		}
		if c.cfg.Log != nil {
			c.cfg.Log.Info("acme account registered",
				zap.String("directory", c.cfg.DirectoryURL),
				zap.String("account_url", a.URI))
		}
	} else {
		cl.KID = acme.KeyID(ak.AccountURL)
	}
	return cl, ak, nil
}

// Obtain 为 hostname 申请证书（http-01 挑战由数据平面代答）。
func (c *Client) Obtain(ctx context.Context, hostname string) (*Issued, error) {
	if c.cfg.Challenge != "" && c.cfg.Challenge != "http-01" {
		return nil, fmt.Errorf("acme challenge %q not implemented (only http-01)", c.cfg.Challenge)
	}
	cl, _, err := c.EnsureAccount(ctx)
	if err != nil {
		return nil, err
	}
	order, err := cl.AuthorizeOrder(ctx, acme.DomainIDs(hostname))
	if err != nil {
		return nil, classify(err)
	}
	if err := c.fulfillAuthorizations(ctx, cl, order, hostname); err != nil {
		return nil, err
	}
	return c.finalize(ctx, cl, order, hostname)
}

// fulfillAuthorizations 完成全部授权（http-01 挑战：登记应答 → 通知 CA → 等待结果 → 清理）。
func (c *Client) fulfillAuthorizations(ctx context.Context, cl *acme.Client, order *acme.Order, hostname string) error {
	for _, authzURL := range order.AuthzURLs {
		z, err := cl.GetAuthorization(ctx, authzURL)
		if err != nil {
			return classify(err)
		}
		if z.Status == acme.StatusValid {
			continue
		}
		chal := pickHTTP01(z.Challenges)
		if chal == nil {
			return fmt.Errorf("%w: no http-01 challenge offered for %s", ErrChallengeFailed, hostname)
		}
		keyAuth, err := cl.HTTP01ChallengeResponse(chal.Token)
		if err != nil {
			return err
		}
		c.cfg.Challenges.Present(chal.Token, keyAuth, challengeTTL)
		if _, err := cl.Accept(ctx, chal); err != nil {
			c.cfg.Challenges.Clear(chal.Token)
			return classify(err)
		}
		if _, err := cl.WaitAuthorization(ctx, authzURL); err != nil {
			c.cfg.Challenges.Clear(chal.Token)
			return classify(err)
		}
		c.cfg.Challenges.Clear(chal.Token)
	}
	return nil
}

// finalize 生成证书密钥 + CSR，提交 finalize 并下载证书链。
func (c *Client) finalize(ctx context.Context, cl *acme.Client, order *acme.Order, hostname string) (*Issued, error) {
	certKey, err := generateKey(c.cfg.KeyType)
	if err != nil {
		return nil, err
	}
	csrDER, err := x509.CreateCertificateRequest(nil, &x509.CertificateRequest{
		DNSNames: []string{hostname},
	}, certKey)
	if err != nil {
		return nil, fmt.Errorf("create csr: %w", err)
	}
	der, _, err := cl.CreateOrderCert(ctx, order.FinalizeURL, csrDER, true)
	if err != nil {
		return nil, classify(err)
	}
	if len(der) == 0 {
		return nil, fmt.Errorf("acme: empty certificate chain")
	}
	leaf, err := x509.ParseCertificate(der[0])
	if err != nil {
		return nil, fmt.Errorf("parse issued leaf: %w", err)
	}
	keyPEM, err := marshalKeyPEM(certKey)
	if err != nil {
		return nil, err
	}
	var chain strings.Builder
	for _, d := range der {
		if err := pem.Encode(&chain, &pem.Block{Type: "CERTIFICATE", Bytes: d}); err != nil {
			return nil, err
		}
	}
	return &Issued{
		CertificatePEM: chain.String(),
		PrivateKeyPEM:  keyPEM,
		NotAfter:       leaf.NotAfter,
		Issuer:         leaf.Issuer.String(),
		SerialNumber:   leaf.SerialNumber.String(),
		OrderURL:       order.URI,
		Challenge:      "http-01",
	}, nil
}

// Revoke 撤销证书（PEM）。reason 为 RFC 5280 撤销原因码。
func (c *Client) Revoke(ctx context.Context, certPEM string, reason int) error {
	cl, ak, err := c.EnsureAccount(ctx)
	if err != nil {
		return err
	}
	block, _ := pem.Decode([]byte(certPEM))
	if block == nil {
		return fmt.Errorf("acme: no PEM block in certificate")
	}
	signer, err := parseKeyPEM(ak.KeyPEM)
	if err != nil {
		return err
	}
	if err := cl.RevokeCert(ctx, signer, block.Bytes, acme.CRLReasonCode(reason)); err != nil {
		return classify(err)
	}
	return nil
}

// pickHTTP01 从挑战列表中选出 http-01。
func pickHTTP01(chs []*acme.Challenge) *acme.Challenge {
	for _, ch := range chs {
		if ch.Type == "http-01" {
			return ch
		}
	}
	return nil
}
