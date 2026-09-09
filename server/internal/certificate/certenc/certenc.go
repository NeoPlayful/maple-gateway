// Package certenc 负责证书私钥的加密存储（AES-256-GCM）。
// 数据密钥从 MAPLE_CERT_ENC_KEY 读取（base64 编码的 32 字节），
// 缺失时拒绝工作，保证私钥明文永不落库。
package certenc

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
)

const keyEnv = "MAPLE_CERT_ENC_KEY"

// keySize AES-256 要求 32 字节密钥。
const keySize = 32

// ErrNoKey 表示加密密钥未配置。
var ErrNoKey = errors.New("MAPLE_CERT_ENC_KEY not set (required for certificate private key encryption)")

type box struct {
	aead cipher.AEAD
}

// New 从环境变量读取数据密钥并构造加解密器。
// 未配置或长度非法时返回 ErrNoKey。
func New() (*box, error) {
	return NewFromEnv(os.Getenv(keyEnv))
}

// NewFromEnv 便于测试注入。
func NewFromEnv(encoded string) (*box, error) {
	if encoded == "" {
		return nil, ErrNoKey
	}
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, fmt.Errorf("decode %s: %w", keyEnv, err)
	}
	if len(raw) != keySize {
		return nil, fmt.Errorf("%s must decode to %d bytes, got %d", keyEnv, keySize, len(raw))
	}
	block, err := aes.NewCipher(raw)
	if err != nil {
		return nil, fmt.Errorf("new cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("new gcm: %w", err)
	}
	return &box{aead: aead}, nil
}

// Encrypt 加密明文私钥，输出 base64 密文（含随机 nonce）。
func (b *box) Encrypt(plaintext []byte) (string, error) {
	nonce := make([]byte, b.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", fmt.Errorf("generate nonce: %w", err)
	}
	sealed := b.aead.Seal(nonce, nonce, plaintext, nil)
	return base64.StdEncoding.EncodeToString(sealed), nil
}

// Decrypt 解密 base64 密文（Encrypt 的逆操作）。
func (b *box) Decrypt(encoded string) ([]byte, error) {
	sealed, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, fmt.Errorf("decode ciphertext: %w", err)
	}
	if len(sealed) < b.aead.NonceSize() {
		return nil, errors.New("ciphertext too short")
	}
	nonce, ciphertext := sealed[:b.aead.NonceSize()], sealed[b.aead.NonceSize():]
	plaintext, err := b.aead.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, fmt.Errorf("decrypt: %w", err)
	}
	return plaintext, nil
}
