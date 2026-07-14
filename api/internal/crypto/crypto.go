// Package crypto provides authenticated symmetric encryption for secrets that
// must be stored at rest, such as the per-workspace provider API keys behind
// BYOK. It is deliberately the only place in the codebase that handles
// encryption, so the key format and ciphertext layout have a single definition.
//
// Unlike the session cookie in internal/auth, which is only *signed* (HMAC) and
// whose payload is meant to be readable by the client, values encrypted here
// must never be recoverable without the master key.
package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
)

// KeySize is the required length, in bytes, of the decoded master key: AES-256.
const KeySize = 32

// ErrInvalidCiphertext is returned when a value cannot be decrypted, whether
// because it is malformed, truncated, or was encrypted under a different key.
// The cause is deliberately not distinguished: telling an attacker *why*
// decryption failed leaks information about the key and the ciphertext.
var ErrInvalidCiphertext = errors.New("crypto: invalid ciphertext")

// Cipher encrypts and decrypts secrets with AES-256-GCM.
//
// Safe for concurrent use: cipher.AEAD is stateless, and a fresh nonce is
// generated per Encrypt call.
type Cipher struct {
	aead cipher.AEAD
}

// New builds a Cipher from a base64-encoded 32-byte master key (the value of
// SECRETS_ENCRYPTION_KEY). Generate one with:
//
//	openssl rand -base64 32
func New(encodedKey string) (*Cipher, error) {
	key, err := base64.StdEncoding.DecodeString(encodedKey)
	if err != nil {
		return nil, fmt.Errorf("decoding encryption key: %w", err)
	}

	if len(key) != KeySize {
		return nil, fmt.Errorf("encryption key must decode to %d bytes, got %d", KeySize, len(key))
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("building aes cipher: %w", err)
	}

	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("building gcm: %w", err)
	}

	return &Cipher{aead: aead}, nil
}

// Encrypt seals plaintext and returns nonce||ciphertext. The nonce is random
// per call, so encrypting the same plaintext twice yields different output.
func (c *Cipher) Encrypt(plaintext []byte) ([]byte, error) {
	nonce := make([]byte, c.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("generating nonce: %w", err)
	}

	// Seal appends the ciphertext to nonce, giving the nonce||ciphertext layout
	// Decrypt expects, in one allocation.
	return c.aead.Seal(nonce, nonce, plaintext, nil), nil
}

// Decrypt opens a nonce||ciphertext value produced by Encrypt. Any failure —
// truncation, tampering, or a wrong key — returns ErrInvalidCiphertext.
func (c *Cipher) Decrypt(sealed []byte) ([]byte, error) {
	nonceSize := c.aead.NonceSize()
	if len(sealed) < nonceSize {
		return nil, ErrInvalidCiphertext
	}

	nonce, ciphertext := sealed[:nonceSize], sealed[nonceSize:]

	plaintext, err := c.aead.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, ErrInvalidCiphertext
	}

	return plaintext, nil
}
