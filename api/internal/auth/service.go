package auth

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"golang.org/x/oauth2"

	"github.com/jpgomesr/neuralvault/api/internal/config"
	"github.com/jpgomesr/neuralvault/api/internal/model"
	"github.com/jpgomesr/neuralvault/api/internal/storage"
)

// Service drives the OIDC authorization-code login flow and resolves the
// authenticated user, provisioning them on first login.
type Service interface {
	// AuthCodeURL returns the provider authorization URL to redirect the user
	// to, carrying the given opaque state.
	AuthCodeURL(state string) string
	// Exchange completes the OIDC callback: it exchanges code for tokens,
	// verifies the ID token, and JIT-provisions the user. It returns the
	// resolved user, whether they were newly created, and any error.
	Exchange(ctx context.Context, code string) (user *model.User, created bool, err error)
	// PasswordLogin authenticates via the OAuth2 Resource Owner Password
	// Credentials grant against the provider's token endpoint, verifies the
	// returned ID token identically to Exchange, and JIT-provisions the user.
	// It returns the resolved user, whether they were newly created, and any
	// error; a rejected email/password pair is reported as
	// ErrInvalidCredentials.
	PasswordLogin(ctx context.Context, email, password string) (user *model.User, created bool, err error)
	// HealthCheck verifies the OIDC provider (Keycloak in dev) is reachable by
	// requesting its discovery document.
	HealthCheck(ctx context.Context) error
}

// ErrInvalidCredentials is returned by PasswordLogin when the provider rejects
// the given email/password pair, as opposed to a provider or network failure.
var ErrInvalidCredentials = errors.New("invalid email or password")

// AuthService implements Service against any standard OIDC provider discovered
// from cfg.Auth.IssuerURL. It contains no provider-specific code.
type AuthService struct {
	pool     storage.Pool
	oauth    oauth2.Config
	verifier *oidc.IDTokenVerifier
	// provider labels the identity source in user_identity; derived from the
	// issuer so the same subject from a different provider stays distinct.
	provider string
	// httpClient probes the issuer's discovery endpoint in HealthCheck.
	httpClient *http.Client
}

// healthCheckTimeout bounds a single discovery request in HealthCheck,
// independent of the caller's context.
const healthCheckTimeout = 5 * time.Second

// NewAuthService builds an AuthService by performing OIDC discovery against
// cfg.Auth.IssuerURL. It requires network access to the issuer at startup.
func NewAuthService(ctx context.Context, cfg *config.Config, pool storage.Pool) (*AuthService, error) {
	provider, err := oidc.NewProvider(ctx, cfg.Auth.IssuerURL)
	if err != nil {
		return nil, fmt.Errorf("oidc discovery: %w", err)
	}
	return &AuthService{
		pool: pool,
		oauth: oauth2.Config{
			ClientID:     cfg.Auth.ClientID,
			ClientSecret: cfg.Auth.ClientSecret,
			RedirectURL:  cfg.Auth.RedirectURL,
			Endpoint:     provider.Endpoint(),
			Scopes:       []string{oidc.ScopeOpenID, "email", "profile"},
		},
		verifier:   provider.Verifier(&oidc.Config{ClientID: cfg.Auth.ClientID}),
		provider:   cfg.Auth.IssuerURL,
		httpClient: &http.Client{Timeout: healthCheckTimeout},
	}, nil
}

// HealthCheck verifies the OIDC provider is reachable by requesting its
// discovery document (.well-known/openid-configuration).
func (s *AuthService) HealthCheck(ctx context.Context) error {
	url := strings.TrimRight(s.provider, "/") + "/.well-known/openid-configuration"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("building oidc discovery request: %w", err)
	}

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("reaching oidc provider: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("oidc discovery: unexpected status %d", resp.StatusCode)
	}
	return nil
}

// AuthCodeURL returns the provider authorization URL for the given state.
func (s *AuthService) AuthCodeURL(state string) string {
	return s.oauth.AuthCodeURL(state)
}

// idClaims are the OIDC ID-token claims this service reads.
type idClaims struct {
	Subject string `json:"sub"`
	Email   string `json:"email"`
	Name    string `json:"name"`
}

// Exchange completes the authorization-code flow and resolves the user.
func (s *AuthService) Exchange(ctx context.Context, code string) (*model.User, bool, error) {
	token, err := s.oauth.Exchange(ctx, code)
	if err != nil {
		return nil, false, fmt.Errorf("code exchange: %w", err)
	}
	return s.resolveToken(ctx, token)
}

// PasswordLogin authenticates via the OAuth2 Resource Owner Password
// Credentials grant (RFC 6749 §4.3) against the provider's token endpoint —
// standard OAuth2, not a Keycloak-specific API. The client secret is sent
// server-side only; it never reaches the browser.
func (s *AuthService) PasswordLogin(ctx context.Context, email, password string) (*model.User, bool, error) {
	token, err := s.oauth.PasswordCredentialsToken(ctx, email, password)
	if err != nil {
		var retrieveErr *oauth2.RetrieveError
		if errors.As(err, &retrieveErr) && retrieveErr.ErrorCode == "invalid_grant" {
			return nil, false, ErrInvalidCredentials
		}
		return nil, false, fmt.Errorf("password grant: %w", err)
	}
	return s.resolveToken(ctx, token)
}

// resolveToken extracts and verifies the ID token carried by token (minted by
// either the authorization-code or password grant), then resolves the user.
func (s *AuthService) resolveToken(ctx context.Context, token *oauth2.Token) (*model.User, bool, error) {
	rawID, ok := token.Extra("id_token").(string)
	if !ok {
		return nil, false, errors.New("no id_token in token response")
	}
	idToken, err := s.verifier.Verify(ctx, rawID)
	if err != nil {
		return nil, false, fmt.Errorf("verifying id token: %w", err)
	}

	var claims idClaims
	if err := idToken.Claims(&claims); err != nil {
		return nil, false, fmt.Errorf("parsing id token claims: %w", err)
	}
	if claims.Subject == "" {
		return nil, false, errors.New("id token missing subject")
	}

	return s.resolveUser(ctx, claims)
}

// resolveUser returns the user linked to the OIDC subject, provisioning a new
// user + identity on first login.
func (s *AuthService) resolveUser(ctx context.Context, claims idClaims) (*model.User, bool, error) {
	user, err := s.findUserByIdentity(ctx, claims.Subject)
	if err == nil {
		return user, false, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, false, fmt.Errorf("looking up identity: %w", err)
	}
	return s.provisionUser(ctx, claims)
}

// findUserByIdentity loads the user linked to (provider, subject) via
// user_identity, returning pgx.ErrNoRows if the identity is not yet linked.
func (s *AuthService) findUserByIdentity(ctx context.Context, subject string) (*model.User, error) {
	var u model.User
	err := s.pool.QueryRow(ctx, `
		SELECT u.id, u.email, u.name, u.created_at, u.updated_at
		FROM users u
		JOIN user_identity ui ON ui.user_id = u.id
		WHERE ui.provider = $1 AND ui.external_id = $2`,
		s.provider, subject,
	).Scan(&u.ID, &u.Email, &u.Name, &u.CreatedAt, &u.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &u, nil
}

// provisionUser creates the users row and its user_identity link atomically
// (JIT provisioning). The unique constraint on (provider, external_id)
// guarantees at most one identity survives a concurrent first login.
func (s *AuthService) provisionUser(ctx context.Context, claims idClaims) (*model.User, bool, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, false, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	now := time.Now()
	user := model.User{
		ID:        uuid.New(),
		Email:     claims.Email,
		Name:      displayName(claims),
		CreatedAt: now,
		UpdatedAt: now,
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO users (id, email, name, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5)`,
		user.ID, user.Email, user.Name, user.CreatedAt, user.UpdatedAt,
	); err != nil {
		return nil, false, fmt.Errorf("insert user: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO user_identity (id, user_id, provider, external_id, created_at)
		VALUES ($1, $2, $3, $4, $5)`,
		uuid.New(), user.ID, s.provider, claims.Subject, now,
	); err != nil {
		return nil, false, fmt.Errorf("insert identity: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, false, fmt.Errorf("commit tx: %w", err)
	}
	return &user, true, nil
}

// displayName picks a human-readable name for a new user, falling back to the
// email or subject when the provider omits a name claim (the column is NOT NULL).
func displayName(claims idClaims) string {
	switch {
	case claims.Name != "":
		return claims.Name
	case claims.Email != "":
		return claims.Email
	default:
		return claims.Subject
	}
}
