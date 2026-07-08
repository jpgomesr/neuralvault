package auth

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/jpgomesr/NeuralVault/internal/model"
)

// fakeService is a Service stand-in that lets a test control the OIDC outcome
// without a live provider.
type fakeService struct {
	authCodeURL string
	user        *model.User
	created     bool
	exchangeErr error
	gotCode     string

	passwordLoginErr error
	gotEmail         string
	gotPassword      string
}

func (f *fakeService) AuthCodeURL(state string) string {
	base := f.authCodeURL
	if base == "" {
		base = "https://issuer.example/authorize"
	}
	return base + "?state=" + state
}

func (f *fakeService) Exchange(_ context.Context, code string) (*model.User, bool, error) {
	f.gotCode = code
	if f.exchangeErr != nil {
		return nil, false, f.exchangeErr
	}
	return f.user, f.created, nil
}

func (f *fakeService) PasswordLogin(_ context.Context, email, password string) (*model.User, bool, error) {
	f.gotEmail = email
	f.gotPassword = password
	if f.passwordLoginErr != nil {
		return nil, false, f.passwordLoginErr
	}
	return f.user, f.created, nil
}

func (f *fakeService) HealthCheck(_ context.Context) error { return nil }

// cookieByName returns the last Set-Cookie with the given name, or nil.
func cookieByName(rec *httptest.ResponseRecorder, name string) *http.Cookie {
	var found *http.Cookie
	for _, c := range rec.Result().Cookies() {
		if c.Name == name {
			found = c
		}
	}
	return found
}

func TestLogin_SetsStateAndRedirects(t *testing.T) {
	h := NewHandler(&fakeService{}, testSecret, false, "/app")

	req := httptest.NewRequest(http.MethodGet, "/auth/login", nil)
	rec := httptest.NewRecorder()
	h.Login(rec, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("status: got %d, want %d", rec.Code, http.StatusFound)
	}
	state := cookieByName(rec, stateCookie)
	if state == nil || state.Value == "" {
		t.Fatal("expected a non-empty state cookie")
	}
	if !state.HttpOnly {
		t.Error("state cookie should be HttpOnly")
	}
	loc := rec.Header().Get("Location")
	if !strings.Contains(loc, "state="+state.Value) {
		t.Fatalf("redirect %q should carry the state cookie value %q", loc, state.Value)
	}
}

func TestCallback_Success(t *testing.T) {
	user := &model.User{ID: uuid.New(), Email: "u@example.com"}
	svc := &fakeService{user: user, created: true}
	h := NewHandler(svc, testSecret, false, "/app")

	req := httptest.NewRequest(http.MethodGet, "/auth/callback?state=abc&code=xyz", nil)
	req.AddCookie(&http.Cookie{Name: stateCookie, Value: "abc"})
	rec := httptest.NewRecorder()
	h.Callback(rec, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("status: got %d, want %d", rec.Code, http.StatusFound)
	}
	if svc.gotCode != "xyz" {
		t.Fatalf("exchanged code: got %q, want %q", svc.gotCode, "xyz")
	}
	if loc := rec.Header().Get("Location"); loc != "/app" {
		t.Fatalf("redirect: got %q, want %q", loc, "/app")
	}

	if state := cookieByName(rec, stateCookie); state == nil || state.MaxAge >= 0 {
		t.Error("expected the single-use state cookie to be cleared")
	}
	session := cookieByName(rec, sessionCookie)
	if session == nil || session.Value == "" {
		t.Fatal("expected a session cookie to be set")
	}
	claims, err := h.signer.Verify(session.Value)
	if err != nil {
		t.Fatalf("session cookie failed to verify: %v", err)
	}
	if claims.UserID != user.ID {
		t.Fatalf("session user: got %s, want %s", claims.UserID, user.ID)
	}
}

func TestCallback_Rejects(t *testing.T) {
	cases := []struct {
		name        string
		cookie      *http.Cookie
		query       string
		exchangeErr error
		wantStatus  int
	}{
		{name: "no state cookie", cookie: nil, query: "?state=abc&code=xyz", wantStatus: http.StatusBadRequest},
		{name: "state mismatch", cookie: &http.Cookie{Name: stateCookie, Value: "abc"}, query: "?state=zzz&code=xyz", wantStatus: http.StatusBadRequest},
		{name: "empty state cookie", cookie: &http.Cookie{Name: stateCookie, Value: ""}, query: "?state=&code=xyz", wantStatus: http.StatusBadRequest},
		{name: "missing code", cookie: &http.Cookie{Name: stateCookie, Value: "abc"}, query: "?state=abc", wantStatus: http.StatusBadRequest},
		{name: "exchange error", cookie: &http.Cookie{Name: stateCookie, Value: "abc"}, query: "?state=abc&code=xyz", exchangeErr: errors.New("boom"), wantStatus: http.StatusUnauthorized},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc := &fakeService{user: &model.User{ID: uuid.New()}, exchangeErr: tc.exchangeErr}
			h := NewHandler(svc, testSecret, false, "/app")

			req := httptest.NewRequest(http.MethodGet, "/auth/callback"+tc.query, nil)
			if tc.cookie != nil {
				req.AddCookie(tc.cookie)
			}
			rec := httptest.NewRecorder()
			h.Callback(rec, req)

			if rec.Code != tc.wantStatus {
				t.Fatalf("status: got %d, want %d", rec.Code, tc.wantStatus)
			}
			if cookieByName(rec, sessionCookie) != nil {
				t.Error("no session cookie should be set on a rejected callback")
			}
		})
	}
}

func TestToken_Success(t *testing.T) {
	user := &model.User{ID: uuid.New(), Email: "u@example.com"}
	svc := &fakeService{user: user, created: true}
	h := NewHandler(svc, testSecret, false, "/app")

	body := strings.NewReader(`{"email":"u@example.com","password":"secret"}`)
	req := httptest.NewRequest(http.MethodPost, "/auth/token", body)
	rec := httptest.NewRecorder()
	h.Token(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status: got %d, want %d", rec.Code, http.StatusNoContent)
	}
	if svc.gotEmail != "u@example.com" || svc.gotPassword != "secret" {
		t.Fatalf("password login called with email=%q password=%q", svc.gotEmail, svc.gotPassword)
	}
	session := cookieByName(rec, sessionCookie)
	if session == nil || session.Value == "" {
		t.Fatal("expected a session cookie to be set")
	}
	claims, err := h.signer.Verify(session.Value)
	if err != nil {
		t.Fatalf("session cookie failed to verify: %v", err)
	}
	if claims.UserID != user.ID {
		t.Fatalf("session user: got %s, want %s", claims.UserID, user.ID)
	}
}

func TestToken_InvalidCredentials(t *testing.T) {
	svc := &fakeService{passwordLoginErr: ErrInvalidCredentials}
	h := NewHandler(svc, testSecret, false, "/app")

	body := strings.NewReader(`{"email":"u@example.com","password":"wrong"}`)
	req := httptest.NewRequest(http.MethodPost, "/auth/token", body)
	rec := httptest.NewRecorder()
	h.Token(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status: got %d, want %d", rec.Code, http.StatusUnauthorized)
	}
	if cookieByName(rec, sessionCookie) != nil {
		t.Error("no session cookie should be set on invalid credentials")
	}
}

func TestToken_ProviderError(t *testing.T) {
	svc := &fakeService{passwordLoginErr: errors.New("boom")}
	h := NewHandler(svc, testSecret, false, "/app")

	body := strings.NewReader(`{"email":"u@example.com","password":"secret"}`)
	req := httptest.NewRequest(http.MethodPost, "/auth/token", body)
	rec := httptest.NewRecorder()
	h.Token(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status: got %d, want %d", rec.Code, http.StatusInternalServerError)
	}
	if cookieByName(rec, sessionCookie) != nil {
		t.Error("no session cookie should be set on a provider error")
	}
}

func TestToken_MissingFields(t *testing.T) {
	cases := []struct {
		name string
		body string
	}{
		{name: "empty body", body: `{}`},
		{name: "missing password", body: `{"email":"u@example.com"}`},
		{name: "missing email", body: `{"password":"secret"}`},
		{name: "malformed json", body: `not json`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc := &fakeService{user: &model.User{ID: uuid.New()}}
			h := NewHandler(svc, testSecret, false, "/app")

			req := httptest.NewRequest(http.MethodPost, "/auth/token", strings.NewReader(tc.body))
			rec := httptest.NewRecorder()
			h.Token(rec, req)

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status: got %d, want %d", rec.Code, http.StatusBadRequest)
			}
		})
	}
}

func TestLogout_ClearsSession(t *testing.T) {
	h := NewHandler(&fakeService{}, testSecret, false, "/app")

	req := httptest.NewRequest(http.MethodPost, "/auth/logout", nil)
	rec := httptest.NewRecorder()
	h.Logout(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status: got %d, want %d", rec.Code, http.StatusNoContent)
	}
	session := cookieByName(rec, sessionCookie)
	if session == nil {
		t.Fatal("expected a session cookie to be written")
	}
	if session.MaxAge >= 0 || session.Value != "" {
		t.Errorf("expected the session cookie to be cleared, got %+v", session)
	}
}

func TestMe(t *testing.T) {
	h := NewHandler(&fakeService{}, testSecret, false, "/app")

	t.Run("with principal", func(t *testing.T) {
		p := Principal{UserID: uuid.New(), Email: "me@example.com"}
		req := httptest.NewRequest(http.MethodGet, "/auth/me", nil).
			WithContext(withPrincipal(context.Background(), p))
		rec := httptest.NewRecorder()
		h.Me(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("status: got %d, want %d", rec.Code, http.StatusOK)
		}
		var body meResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("decoding body: %v", err)
		}
		if body.ID != p.UserID.String() || body.Email != p.Email {
			t.Fatalf("body: got %+v, want id=%s email=%s", body, p.UserID, p.Email)
		}
	})

	t.Run("without principal", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/auth/me", nil)
		rec := httptest.NewRecorder()
		h.Me(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("status: got %d, want %d", rec.Code, http.StatusUnauthorized)
		}
	})
}
