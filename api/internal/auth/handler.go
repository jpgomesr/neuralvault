package auth

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/jpgomesr/NeuralVault/internal/httperr"
	"github.com/jpgomesr/NeuralVault/internal/logger"
	"github.com/jpgomesr/NeuralVault/internal/model"
)

const (
	// sessionCookie holds the signed session credential.
	sessionCookie = "nv_session"
	// stateCookie holds the OAuth2 CSRF state during the login round-trip.
	stateCookie = "nv_oauth_state"
	// stateTTL bounds how long a login round-trip may take.
	stateTTL = 10 * time.Minute
)

// tokenSigner issues and verifies session cookies. *sessionSigner is the
// production implementation; tests inject fakes to exercise failure paths.
type tokenSigner interface {
	Issue(userID uuid.UUID, email string) (string, error)
	Verify(token string) (Claims, error)
}

// Handler holds HTTP handler methods and the RequireUser middleware for the
// auth domain.
type Handler struct {
	service      Service
	signer       tokenSigner
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

// Login godoc
//
// It sets a CSRF state cookie and redirects the browser to the OIDC provider's
// authorization endpoint.
//
// @Summary Start OIDC login
// @Description Sets a CSRF state cookie and redirects to the OIDC provider's authorization endpoint (authorization-code flow).
// @Tags auth
// @Success 302
// @Failure 500
// @Router /auth/login [get]
func (h *Handler) Login(w http.ResponseWriter, r *http.Request) {
	state, err := randomState()
	if err != nil {
		httperr.Internal(w, r, "generating oauth state failed", err)
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

// Callback godoc
//
// It validates the CSRF state, exchanges the authorization code, provisions the
// user on first login, sets the session cookie, and redirects to postLoginURL.
//
// @Summary OIDC callback
// @Description Validates the CSRF state, exchanges the authorization code, provisions the user on first login (JIT), sets the nv_session cookie, and redirects to the post-login URL.
// @Tags auth
// @Param state query string true "CSRF state issued by /auth/login"
// @Param code query string true "OIDC authorization code"
// @Success 302
// @Failure 400
// @Failure 401
// @Failure 500
// @Router /auth/callback [get]
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

	if err := h.issueSession(w, user); err != nil {
		httperr.Internal(w, r, "issuing session failed", err, "user_id", user.ID)
		return
	}

	slog.InfoContext(r.Context(), "login succeeded", "user_id", user.ID, "provisioned", created, "request_id", logger.RequestID(r.Context()))
	http.Redirect(w, r, h.postLoginURL, http.StatusFound)
}

// tokenRequest is the JSON body for POST /auth/token.
type tokenRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

// Token godoc
//
// It authenticates the given email/password directly against the OIDC
// provider via the Resource Owner Password Credentials grant (server-side;
// the client secret never reaches the browser), then issues the same
// nv_session cookie used by the authorization-code flow. It exists alongside
// (not in place of) GET /auth/login for a native, non-redirecting sign-in UI.
//
// @Summary Native email/password login
// @Description Authenticates against the OIDC provider's token endpoint via the password grant and issues the nv_session cookie.
// @Tags auth
// @Accept json
// @Param body body tokenRequest true "Credentials"
// @Success 204
// @Failure 400
// @Failure 401
// @Failure 500
// @Router /auth/token [post]
func (h *Handler) Token(w http.ResponseWriter, r *http.Request) {
	var req tokenRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Email == "" || req.Password == "" {
		http.Error(w, "email and password are required", http.StatusBadRequest)
		return
	}

	user, created, err := h.service.PasswordLogin(r.Context(), req.Email, req.Password)
	if err != nil {
		if errors.Is(err, ErrInvalidCredentials) {
			http.Error(w, "invalid email or password", http.StatusUnauthorized)
			return
		}
		httperr.Internal(w, r, "password login failed", err)
		return
	}

	if err := h.issueSession(w, user); err != nil {
		httperr.Internal(w, r, "issuing session failed", err, "user_id", user.ID)
		return
	}

	slog.InfoContext(r.Context(), "password login succeeded", "user_id", user.ID, "provisioned", created, "request_id", logger.RequestID(r.Context()))
	w.WriteHeader(http.StatusNoContent)
}

// issueSession signs a session token for user and sets the nv_session cookie.
func (h *Handler) issueSession(w http.ResponseWriter, user *model.User) error {
	token, err := h.signer.Issue(user.ID, user.Email)
	if err != nil {
		return err
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
	return nil
}

// Logout godoc
//
// @Summary Log out
// @Description Clears the nv_session cookie.
// @Tags auth
// @Success 204
// @Router /auth/logout [post]
func (h *Handler) Logout(w http.ResponseWriter, r *http.Request) {
	h.clearCookie(w, sessionCookie)
	w.WriteHeader(http.StatusNoContent)
}

// meResponse is the JSON body returned by GET /auth/me.
type meResponse struct {
	ID    string `json:"id"`
	Email string `json:"email"`
}

// Me godoc
//
// It is mounted behind RequireUser, so the principal is always present.
//
// @Summary Current user
// @Description Returns the id and email of the authenticated caller.
// @Tags auth
// @Produce json
// @Success 200
// @Failure 401
// @Router /auth/me [get]
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
