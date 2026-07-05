package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// sessionTTL is how long an issued session cookie remains valid.
const sessionTTL = 24 * time.Hour

var (
	// ErrInvalidToken is returned when a session token is malformed or its
	// signature does not verify.
	ErrInvalidToken = errors.New("invalid session token")
	// ErrExpiredToken is returned when a session token's expiry has passed.
	ErrExpiredToken = errors.New("session token expired")
)

// Claims is the payload carried by the session cookie.
type Claims struct {
	UserID uuid.UUID `json:"uid"`
	Email  string    `json:"email"`
	Expiry int64     `json:"exp"`
}

// sessionSigner issues and verifies stateless session tokens signed with
// HMAC-SHA256. Token format: base64url(payloadJSON) "." base64url(hmac).
// This avoids a JWT dependency while giving a tamper-evident, self-contained
// session credential suitable for an HttpOnly cookie.
type sessionSigner struct {
	secret []byte
	ttl    time.Duration
	now    func() time.Time
}

// newSessionSigner returns a signer keyed on secret with the default TTL.
func newSessionSigner(secret string) *sessionSigner {
	return &sessionSigner{secret: []byte(secret), ttl: sessionTTL, now: time.Now}
}

// Issue returns a signed session token for the given user.
func (s *sessionSigner) Issue(userID uuid.UUID, email string) (string, error) {
	claims := Claims{
		UserID: userID,
		Email:  email,
		Expiry: s.now().Add(s.ttl).Unix(),
	}
	payload, err := json.Marshal(claims)
	if err != nil {
		return "", fmt.Errorf("marshaling claims: %w", err)
	}
	encoded := base64.RawURLEncoding.EncodeToString(payload)
	return encoded + "." + s.sign(encoded), nil
}

// Verify checks a session token's signature and expiry, returning its claims.
func (s *sessionSigner) Verify(token string) (Claims, error) {
	encoded, sig, found := strings.Cut(token, ".")
	if !found {
		return Claims{}, ErrInvalidToken
	}
	// Constant-time comparison guards against signature timing attacks.
	if !hmac.Equal([]byte(sig), []byte(s.sign(encoded))) {
		return Claims{}, ErrInvalidToken
	}
	payload, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return Claims{}, ErrInvalidToken
	}
	var claims Claims
	if err := json.Unmarshal(payload, &claims); err != nil {
		return Claims{}, ErrInvalidToken
	}
	if s.now().Unix() >= claims.Expiry {
		return Claims{}, ErrExpiredToken
	}
	return claims, nil
}

// sign returns the base64url-encoded HMAC-SHA256 of encoded.
func (s *sessionSigner) sign(encoded string) string {
	mac := hmac.New(sha256.New, s.secret)
	mac.Write([]byte(encoded))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}
