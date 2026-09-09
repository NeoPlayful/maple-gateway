package certenc

import (
	"encoding/base64"
	"testing"
)

func testKey(t *testing.T) string {
	t.Helper()
	raw := make([]byte, 32)
	for i := range raw {
		raw[i] = byte(i)
	}
	return base64.StdEncoding.EncodeToString(raw)
}

func TestEncryptDecryptRoundTrip(t *testing.T) {
	b, err := NewFromEnv(testKey(t))
	if err != nil {
		t.Fatalf("NewFromEnv: %v", err)
	}
	plain := []byte("-----BEGIN PRIVATE KEY-----\n...secret...\n-----END PRIVATE KEY-----")
	enc, err := b.Encrypt(plain)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	if enc == string(plain) {
		t.Fatal("ciphertext must not equal plaintext")
	}
	dec, err := b.Decrypt(enc)
	if err != nil {
		t.Fatalf("Decrypt: %v", err)
	}
	if string(dec) != string(plain) {
		t.Fatal("round trip mismatch")
	}
}

func TestEncryptRandomNonce(t *testing.T) {
	b, _ := NewFromEnv(testKey(t))
	a, _ := b.Encrypt([]byte("same plaintext"))
	c, _ := b.Encrypt([]byte("same plaintext"))
	if a == c {
		t.Fatal("two encryptions of same plaintext must differ (random nonce)")
	}
}

func TestNoKeyRejected(t *testing.T) {
	if _, err := NewFromEnv(""); err == nil {
		t.Fatal("empty key must be rejected")
	}
}

func TestWrongKeyLengthRejected(t *testing.T) {
	if _, err := NewFromEnv(base64.StdEncoding.EncodeToString([]byte("short"))); err == nil {
		t.Fatal("non-32-byte key must be rejected")
	}
}

func TestTamperedCiphertextFails(t *testing.T) {
	b, _ := NewFromEnv(testKey(t))
	enc, err := b.Encrypt([]byte("secret"))
	if err != nil {
		t.Fatal(err)
	}
	// 翻转密文中间一个字节，解密应失败。
	raw, _ := base64.StdEncoding.DecodeString(enc)
	raw[len(raw)/2] ^= 0xFF
	tampered := base64.StdEncoding.EncodeToString(raw)
	if _, err := b.Decrypt(tampered); err == nil {
		t.Fatal("tampered ciphertext must fail to decrypt")
	}
}
