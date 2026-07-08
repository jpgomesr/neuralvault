package auth

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/jpgomesr/NeuralVault/internal/config"
)

// stubIDP is a minimal OIDC provider backed by httptest: it serves a discovery
// document, a JWKS, and a token endpoint that mints a signed ID token. It lets
// the tests drive NewAuthService (discovery) and Exchange (code exchange + token
// verification) end-to-end without a real Keycloak, using only the stdlib.
type stubIDP struct {
	issuer   string
	clientID string
	key      *rsa.PrivateKey

	// knobs for exercising failure branches
	signKey         *rsa.PrivateKey // signs the ID token; defaults to key (matches JWKS)
	subject         string
	email           string
	name            string
	omitIDToken     bool // token response carries no id_token
	tokenFailure    bool // token endpoint returns 400 (code exchange)
	passwordDenied  bool // token endpoint returns 400 invalid_grant (password grant)
	gotGrantType    string
	gotUsername     string
	gotPassword     string
}

func newStubIDP(t *testing.T, clientID string) *stubIDP {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	idp := &stubIDP{
		clientID: clientID,
		key:      key,
		subject:  "sub-" + uuid.NewString(),
		email:    "oidc-user@example.com",
		name:     "OIDC User",
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, map[string]any{
			"issuer":                                idp.issuer,
			"authorization_endpoint":                idp.issuer + "/authorize",
			"token_endpoint":                        idp.issuer + "/token",
			"jwks_uri":                              idp.issuer + "/jwks",
			"response_types_supported":              []string{"code"},
			"subject_types_supported":               []string{"public"},
			"id_token_signing_alg_values_supported": []string{"RS256"},
		})
	})
	mux.HandleFunc("/jwks", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, idp.jwks())
	})
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		idp.gotGrantType = r.FormValue("grant_type")
		idp.gotUsername = r.FormValue("username")
		idp.gotPassword = r.FormValue("password")

		if idp.gotGrantType == "password" && idp.passwordDenied {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "invalid_grant"})
			return
		}
		if idp.tokenFailure {
			http.Error(w, "invalid_grant", http.StatusBadRequest)
			return
		}
		resp := map[string]any{
			"access_token": "test-access-token",
			"token_type":   "Bearer",
			"expires_in":   3600,
		}
		if !idp.omitIDToken {
			resp["id_token"] = idp.signIDToken(t)
		}
		writeJSON(w, resp)
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	idp.issuer = srv.URL
	return idp
}

// jwks publishes the public half of key, so a token signed by key verifies.
func (s *stubIDP) jwks() map[string]any {
	pub := s.key.Public().(*rsa.PublicKey)
	return map[string]any{
		"keys": []map[string]any{{
			"kty": "RSA",
			"kid": "test-key",
			"use": "sig",
			"alg": "RS256",
			"n":   base64.RawURLEncoding.EncodeToString(pub.N.Bytes()),
			"e":   base64.RawURLEncoding.EncodeToString(big.NewInt(int64(pub.E)).Bytes()),
		}},
	}
}

// signIDToken builds and signs an RS256 JWT with the configured claims.
func (s *stubIDP) signIDToken(t *testing.T) string {
	t.Helper()
	signer := s.signKey
	if signer == nil {
		signer = s.key
	}
	now := time.Now()
	header := map[string]any{"alg": "RS256", "typ": "JWT", "kid": "test-key"}
	claims := map[string]any{
		"iss":   s.issuer,
		"aud":   s.clientID,
		"sub":   s.subject,
		"email": s.email,
		"name":  s.name,
		"iat":   now.Unix(),
		"exp":   now.Add(time.Hour).Unix(),
	}

	hb, _ := json.Marshal(header)
	cb, _ := json.Marshal(claims)
	signingInput := b64(hb) + "." + b64(cb)

	digest := sha256.Sum256([]byte(signingInput))
	sig, err := rsa.SignPKCS1v15(rand.Reader, signer, crypto.SHA256, digest[:])
	if err != nil {
		t.Fatalf("sign id token: %v", err)
	}
	return signingInput + "." + b64(sig)
}

func b64(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

// serviceFromIDP discovers idp and returns the AuthService under test.
func serviceFromIDP(t *testing.T, idp *stubIDP) *AuthService {
	t.Helper()
	cfg := &config.Config{Auth: config.Auth{
		IssuerURL:    idp.issuer,
		ClientID:     idp.clientID,
		ClientSecret: "test-secret",
		RedirectURL:  "https://app.example/callback",
	}}
	svc, err := NewAuthService(context.Background(), cfg, sharedPool)
	if err != nil {
		t.Fatalf("NewAuthService: %v", err)
	}
	return svc
}

func TestNewAuthService_DiscoveryError(t *testing.T) {
	// A closed port makes discovery fail fast.
	cfg := &config.Config{Auth: config.Auth{IssuerURL: "http://127.0.0.1:1/no-issuer"}}

	_, err := NewAuthService(context.Background(), cfg, sharedPool)
	if err == nil || !strings.Contains(err.Error(), "oidc discovery") {
		t.Fatalf("NewAuthService: got %v, want an oidc discovery error", err)
	}
}

func TestExchange_ProvisionsUser(t *testing.T) {
	ctx := context.Background()
	idp := newStubIDP(t, "test-client")
	svc := serviceFromIDP(t, idp)

	user, created, err := svc.Exchange(ctx, "auth-code")
	if err != nil {
		t.Fatalf("Exchange: %v", err)
	}
	cleanupUser(t, user.ID)

	if !created {
		t.Error("expected the user to be provisioned on first login")
	}
	if user.Email != idp.email || user.Name != idp.name {
		t.Fatalf("resolved user: got %+v, want email=%s name=%s", user, idp.email, idp.name)
	}

	// A second exchange with the same subject reuses the existing user.
	again, created, err := svc.Exchange(ctx, "auth-code")
	if err != nil {
		t.Fatalf("second Exchange: %v", err)
	}
	if created {
		t.Error("expected the second login to reuse the existing user")
	}
	if again.ID != user.ID {
		t.Fatalf("second login resolved user %s, want %s", again.ID, user.ID)
	}
}

func TestExchange_CodeExchangeError(t *testing.T) {
	idp := newStubIDP(t, "test-client")
	idp.tokenFailure = true
	svc := serviceFromIDP(t, idp)

	_, _, err := svc.Exchange(context.Background(), "auth-code")
	if err == nil || !strings.Contains(err.Error(), "code exchange") {
		t.Fatalf("Exchange: got %v, want a code exchange error", err)
	}
}

func TestExchange_NoIDToken(t *testing.T) {
	idp := newStubIDP(t, "test-client")
	idp.omitIDToken = true
	svc := serviceFromIDP(t, idp)

	_, _, err := svc.Exchange(context.Background(), "auth-code")
	if err == nil || !strings.Contains(err.Error(), "no id_token") {
		t.Fatalf("Exchange: got %v, want a missing id_token error", err)
	}
}

func TestExchange_InvalidSignature(t *testing.T) {
	idp := newStubIDP(t, "test-client")
	// Sign the ID token with a key that is not published in the JWKS.
	otherKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	idp.signKey = otherKey
	svc := serviceFromIDP(t, idp)

	_, _, err = svc.Exchange(context.Background(), "auth-code")
	if err == nil || !strings.Contains(err.Error(), "verifying id token") {
		t.Fatalf("Exchange: got %v, want a verifying id token error", err)
	}
}

func TestExchange_MissingSubject(t *testing.T) {
	idp := newStubIDP(t, "test-client")
	idp.subject = "" // token verifies, but carries no subject
	svc := serviceFromIDP(t, idp)

	_, _, err := svc.Exchange(context.Background(), "auth-code")
	if err == nil || !strings.Contains(err.Error(), "missing subject") {
		t.Fatalf("Exchange: got %v, want a missing subject error", err)
	}
}

func TestPasswordLogin_ProvisionsUser(t *testing.T) {
	ctx := context.Background()
	idp := newStubIDP(t, "test-client")
	svc := serviceFromIDP(t, idp)

	user, created, err := svc.PasswordLogin(ctx, "dev@neuralvault.local", "dev")
	if err != nil {
		t.Fatalf("PasswordLogin: %v", err)
	}
	cleanupUser(t, user.ID)

	if !created {
		t.Error("expected the user to be provisioned on first login")
	}
	if idp.gotGrantType != "password" || idp.gotUsername != "dev@neuralvault.local" || idp.gotPassword != "dev" {
		t.Fatalf("token request: got grant_type=%q username=%q password=%q", idp.gotGrantType, idp.gotUsername, idp.gotPassword)
	}
	if user.Email != idp.email || user.Name != idp.name {
		t.Fatalf("resolved user: got %+v, want email=%s name=%s", user, idp.email, idp.name)
	}

	// A second login with the same subject reuses the existing user.
	again, created, err := svc.PasswordLogin(ctx, "dev@neuralvault.local", "dev")
	if err != nil {
		t.Fatalf("second PasswordLogin: %v", err)
	}
	if created {
		t.Error("expected the second login to reuse the existing user")
	}
	if again.ID != user.ID {
		t.Fatalf("second login resolved user %s, want %s", again.ID, user.ID)
	}
}

func TestPasswordLogin_InvalidCredentials(t *testing.T) {
	idp := newStubIDP(t, "test-client")
	idp.passwordDenied = true
	svc := serviceFromIDP(t, idp)

	_, _, err := svc.PasswordLogin(context.Background(), "dev@neuralvault.local", "wrong")
	if !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("PasswordLogin: got %v, want ErrInvalidCredentials", err)
	}
}

func TestPasswordLogin_NoIDToken(t *testing.T) {
	idp := newStubIDP(t, "test-client")
	idp.omitIDToken = true
	svc := serviceFromIDP(t, idp)

	_, _, err := svc.PasswordLogin(context.Background(), "dev@neuralvault.local", "dev")
	if err == nil || !strings.Contains(err.Error(), "no id_token") {
		t.Fatalf("PasswordLogin: got %v, want a missing id_token error", err)
	}
}

func TestPasswordLogin_InvalidSignature(t *testing.T) {
	idp := newStubIDP(t, "test-client")
	otherKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	idp.signKey = otherKey
	svc := serviceFromIDP(t, idp)

	_, _, err = svc.PasswordLogin(context.Background(), "dev@neuralvault.local", "dev")
	if err == nil || !strings.Contains(err.Error(), "verifying id token") {
		t.Fatalf("PasswordLogin: got %v, want a verifying id token error", err)
	}
}

func TestPasswordLogin_MissingSubject(t *testing.T) {
	idp := newStubIDP(t, "test-client")
	idp.subject = ""
	svc := serviceFromIDP(t, idp)

	_, _, err := svc.PasswordLogin(context.Background(), "dev@neuralvault.local", "dev")
	if err == nil || !strings.Contains(err.Error(), "missing subject") {
		t.Fatalf("PasswordLogin: got %v, want a missing subject error", err)
	}
}
