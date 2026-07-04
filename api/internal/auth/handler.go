package auth

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/jpgomesr/NeuralVault/internal/logger"
)

const (
	// sessionCookie holds the signed session credential.
	sessionCookie = "nv_session"
	// stateCookie holds the OAuth2 CSRF state during the login round-trip.
	stateCookie = "nv_oauth_state"
	// stateTTL bounds how long a login round-trip may take.
	stateTTL = 10 * time.Minute
)

// Handler holds HTTP handler methods and the RequireUser middleware for the
// auth domain.
type Handler struct {
	service      Service
	signer       *sessionSigner
	cookieSecure bool
	postLoginURL string
}

// NewHandler returns a Handler. secret signs the session cookie (HMAC-SHA256);
// cookieSecure marks cookies Secure (enable behind HTTPS); postLoginURL is
// where the browser lands after a successful login.
func NewHandler(service Service, secret string, cookieSecure bool, postLoginURL string) *Handler {
	return &Handler{
		service:      service,
		signer:       newSessionSigner(secret),
		cookieSecure: cookieSecure,
		postLoginURL: postLoginURL,
	}
}

// Login handles GET /auth/login. It sets a CSRF state cookie and redirects the
// browser to the OIDC provider's authorization endpoint.
func (h *Handler) Login(w http.ResponseWriter, r *http.Request) {
	state, err := randomState()
	if err != nil {
		slog.ErrorContext(r.Context(), "generating oauth state failed", "err", err, "request_id", logger.RequestID(r.Context()))
		http.Error(w, "failed to start login", http.StatusInternalServerError)
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     stateCookie,
		Value:    state,
		Path:     "/",
		MaxAge:   int(stateTTL.Seconds()),
		HttpOnly: true,
		Secure:   h.cookieSecure,
		SameSite: http.SameSiteLaxMode,
	})

	http.Redirect(w, r, h.service.AuthCodeURL(state), http.StatusFound)
}

// Callback handles GET /auth/callback. It validates the CSRF state, exchanges
// the authorization code, provisions the user on first login, sets the session
// cookie, and redirects to postLoginURL.
func (h *Handler) Callback(w http.ResponseWriter, r *http.Request) {
	stateCk, err := r.Cookie(stateCookie)
	if err != nil || stateCk.Value == "" || stateCk.Value != r.URL.Query().Get("state") {
		slog.WarnContext(r.Context(), "oauth state mismatch", "request_id", logger.RequestID(r.Context()))
		http.Error(w, "invalid or expired login state", http.StatusBadRequest)
		return
	}
	// The state cookie is single-use; clear it regardless of outcome.
	h.clearCookie(w, stateCookie)

	code := r.URL.Query().Get("code")
	if code == "" {
		http.Error(w, "missing authorization code", http.StatusBadRequest)
		return
	}

	user, created, err := h.service.Exchange(r.Context(), code)
	if err != nil {
		slog.ErrorContext(r.Context(), "oidc exchange failed", "err", err, "request_id", logger.RequestID(r.Context()))
		http.Error(w, "authentication failed", http.StatusUnauthorized)
		return
	}

	token, err := h.signer.Issue(user.ID, user.Email)
	if err != nil {
		slog.ErrorContext(r.Context(), "issuing session failed", "err", err, "user_id", user.ID, "request_id", logger.RequestID(r.Context()))
		http.Error(w, "authentication failed", http.StatusInternalServerError)
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    token,
		Path:     "/",
		MaxAge:   int(sessionTTL.Seconds()),
		HttpOnly: true,
		Secure:   h.cookieSecure,
		SameSite: http.SameSiteLaxMode,
	})

	slog.InfoContext(r.Context(), "login succeeded", "user_id", user.ID, "provisioned", created, "request_id", logger.RequestID(r.Context()))
	http.Redirect(w, r, h.postLoginURL, http.StatusFound)
}

// Logout handles POST /auth/logout by clearing the session cookie.
func (h *Handler) Logout(w http.ResponseWriter, r *http.Request) {
	h.clearCookie(w, sessionCookie)
	w.WriteHeader(http.StatusNoContent)
}

// meResponse is the JSON body returned by GET /auth/me.
type meResponse struct {
	ID    string `json:"id"`
	Email string `json:"email"`
}

// Me handles GET /auth/me, returning the authenticated caller. It is mounted
// behind RequireUser, so the principal is always present.
func (h *Handler) Me(w http.ResponseWriter, r *http.Request) {
	p, ok := principalFrom(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(meResponse{ID: p.UserID.String(), Email: p.Email}) //nolint:errcheck
}

// clearCookie expires a cookie by name.
func (h *Handler) clearCookie(w http.ResponseWriter, name string) {
	http.SetCookie(w, &http.Cookie{
		Name:     name,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   h.cookieSecure,
		SameSite: http.SameSiteLaxMode,
	})
}

// randomState returns a URL-safe, cryptographically random state string.
func randomState() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
