package crypto_test

import (
	"bytes"
	"encoding/base64"
	"errors"
	"strings"
	"testing"

	"github.com/jpgomesr/neuralvault/api/internal/crypto"
)

// testKey is a valid base64-encoded 32-byte key.
var testKey = base64.StdEncoding.EncodeToString(bytes.Repeat([]byte("a"), crypto.KeySize))

func TestNewRejectsBadKeys(t *testing.T) {
	tests := map[string]string{
		"not base64": "!!!not-base64!!!",
		"too short":  base64.StdEncoding.EncodeToString(bytes.Repeat([]byte("a"), 16)),
		"too long":   base64.StdEncoding.EncodeToString(bytes.Repeat([]byte("a"), 64)),
		"empty":      "",
	}

	for name, key := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := crypto.New(key); err == nil {
				t.Fatalf("New(%q) = nil error, want error", key)
			}
		})
	}
}

func TestEncryptDecryptRoundTrip(t *testing.T) {
	c, err := crypto.New(testKey)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	plaintext := []byte("sk-ant-api03-super-secret-key")

	sealed, err := c.Encrypt(plaintext)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}

	if bytes.Contains(sealed, plaintext) {
		t.Fatal("ciphertext contains the plaintext")
	}

	got, err := c.Decrypt(sealed)
	if err != nil {
		t.Fatalf("Decrypt: %v", err)
	}

	if !bytes.Equal(got, plaintext) {
		t.Fatalf("Decrypt = %q, want %q", got, plaintext)
	}
}

// A fresh nonce per call means the same plaintext must never encrypt to the
// same ciphertext twice — otherwise equal keys would be detectable at rest.
func TestEncryptIsNonDeterministic(t *testing.T) {
	c, err := crypto.New(testKey)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	first, err := c.Encrypt([]byte("same input"))
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	second, err := c.Encrypt([]byte("same input"))
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}

	if bytes.Equal(first, second) {
		t.Fatal("encrypting the same plaintext twice produced identical ciphertext")
	}
}

func TestDecryptRejectsInvalidCiphertext(t *testing.T) {
	c, err := crypto.New(testKey)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	sealed, err := c.Encrypt([]byte("secret"))
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}

	tampered := bytes.Clone(sealed)
	tampered[len(tampered)-1] ^= 0xff

	otherKey := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte("b"), crypto.KeySize))
	other, err := crypto.New(otherKey)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	tests := map[string]struct {
		cipher *crypto.Cipher
		input  []byte
	}{
		"tampered":  {cipher: c, input: tampered},
		"truncated": {cipher: c, input: sealed[:4]},
		"empty":     {cipher: c, input: nil},
		"wrong key": {cipher: other, input: sealed},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := tt.cipher.Decrypt(tt.input); !errors.Is(err, crypto.ErrInvalidCiphertext) {
				t.Fatalf("Decrypt error = %v, want ErrInvalidCiphertext", err)
			}
		})
	}
}

// The error must not leak why decryption failed.
func TestDecryptErrorLeaksNothing(t *testing.T) {
	c, err := crypto.New(testKey)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	_, err = c.Decrypt([]byte("garbage-that-is-long-enough-to-have-a-nonce"))
	if err == nil {
		t.Fatal("Decrypt of garbage = nil error, want error")
	}

	if strings.Contains(err.Error(), "garbage") {
		t.Fatalf("error message echoes the input: %v", err)
	}
}
