package auth

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
	"golang.org/x/oauth2"

	"github.com/jpgomesr/NeuralVault/internal/config"
	pgstore "github.com/jpgomesr/NeuralVault/internal/storage/postgres"
)

// testProvider labels the identity source in the user_identity table for tests.
const testProvider = "https://issuer.test"

var sharedPool *pgxpool.Pool

func TestMain(m *testing.M) {
	os.Exit(runAllTests(m))
}

func runAllTests(m *testing.M) int {
	ctx := context.Background()

	pgCtr, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        "postgres:17",
			ExposedPorts: []string{"5432/tcp"},
			Env: map[string]string{
				"POSTGRES_USER":     "neuralvault",
				"POSTGRES_PASSWORD": "neuralvault",
				"POSTGRES_DB":       "neuralvault",
			},
			WaitingFor: wait.ForLog("database system is ready to accept connections").WithOccurrence(2),
		},
		Started: true,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "start postgres: %v\n", err)
		return 1
	}
	defer func() { _ = pgCtr.Terminate(ctx) }()

	pgHost, err := pgCtr.Host(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "postgres host: %v\n", err)
		return 1
	}
	pgPort, err := pgCtr.MappedPort(ctx, "5432")
	if err != nil {
		fmt.Fprintf(os.Stderr, "postgres port: %v\n", err)
		return 1
	}

	sharedPool, err = pgstore.NewPool(ctx, config.Config{
		Postgres: config.Postgres{
			Host:     pgHost,
			Port:     int(pgPort.Num()),
			Username: "neuralvault",
			Password: "neuralvault",
			Name:     "neuralvault",
			SSLMode:  "disable",
			MaxConns: 10,
			MinConns: 1,
		},
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "create pool: %v\n", err)
		return 1
	}
	defer sharedPool.Close()

	sqlDB := stdlib.OpenDBFromPool(sharedPool)
	defer sqlDB.Close() //nolint:errcheck

	if err := goose.SetDialect("postgres"); err != nil {
		fmt.Fprintf(os.Stderr, "goose dialect: %v\n", err)
		return 1
	}
	wd, _ := os.Getwd()
	migrationsDir := filepath.Join(wd, "../storage/postgres/migrations")
	if err := goose.Up(sqlDB, migrationsDir); err != nil {
		fmt.Fprintf(os.Stderr, "goose up: %v\n", err)
		return 1
	}

	return m.Run()
}

// newDBService returns an AuthService wired only to the shared pool and a fixed
// provider label — enough to exercise the identity-resolution paths without a
// live OIDC provider.
func newDBService() *AuthService {
	return &AuthService{pool: sharedPool, provider: testProvider}
}

// cleanupUser schedules deletion of a user; the user_identity FK cascades.
func cleanupUser(t *testing.T, id uuid.UUID) {
	t.Helper()
	t.Cleanup(func() {
		_, _ = sharedPool.Exec(context.Background(), "DELETE FROM users WHERE id = $1", id)
	})
}

func TestResolveUser_ProvisionsThenReuses(t *testing.T) {
	ctx := context.Background()
	s := newDBService()
	claims := idClaims{
		Subject: "sub-" + uuid.NewString(),
		Email:   "new@example.com",
		Name:    "New User",
	}

	user, created, err := s.resolveUser(ctx, claims)
	if err != nil {
		t.Fatalf("first resolveUser: %v", err)
	}
	cleanupUser(t, user.ID)
	if !created {
		t.Fatal("expected the user to be newly provisioned on first login")
	}
	if user.Email != claims.Email || user.Name != claims.Name {
		t.Fatalf("provisioned user: got %+v, want email=%s name=%s", user, claims.Email, claims.Name)
	}

	// The identity row must link the subject to the new user.
	var linkedID uuid.UUID
	err = sharedPool.QueryRow(ctx,
		"SELECT user_id FROM user_identity WHERE provider = $1 AND external_id = $2",
		testProvider, claims.Subject,
	).Scan(&linkedID)
	if err != nil {
		t.Fatalf("querying identity: %v", err)
	}
	if linkedID != user.ID {
		t.Fatalf("identity links user %s, want %s", linkedID, user.ID)
	}

	// A second login with the same subject resolves the existing user.
	again, created, err := s.resolveUser(ctx, claims)
	if err != nil {
		t.Fatalf("second resolveUser: %v", err)
	}
	if created {
		t.Fatal("expected the second login to reuse the existing user")
	}
	if again.ID != user.ID {
		t.Fatalf("second login resolved user %s, want %s", again.ID, user.ID)
	}
}

func TestFindUserByIdentity_NotLinked(t *testing.T) {
	ctx := context.Background()
	s := newDBService()

	_, err := s.findUserByIdentity(ctx, "unknown-subject-"+uuid.NewString())
	if err == nil {
		t.Fatal("expected an error for an unlinked identity")
	}
}

func TestDisplayName(t *testing.T) {
	cases := []struct {
		name   string
		claims idClaims
		want   string
	}{
		{name: "prefers name", claims: idClaims{Name: "Ada", Email: "ada@example.com", Subject: "sub"}, want: "Ada"},
		{name: "falls back to email", claims: idClaims{Email: "ada@example.com", Subject: "sub"}, want: "ada@example.com"},
		{name: "falls back to subject", claims: idClaims{Subject: "sub"}, want: "sub"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := displayName(tc.claims); got != tc.want {
				t.Fatalf("displayName: got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestAuthCodeURL(t *testing.T) {
	s := &AuthService{
		oauth: oauth2.Config{
			ClientID:    "test-client",
			RedirectURL: "https://app.example/callback",
			Endpoint:    oauth2.Endpoint{AuthURL: "https://issuer.test/authorize"},
			Scopes:      []string{"openid", "email"},
		},
	}

	got := s.AuthCodeURL("opaque-state")
	for _, want := range []string{"https://issuer.test/authorize", "state=opaque-state", "client_id=test-client"} {
		if !strings.Contains(got, want) {
			t.Fatalf("AuthCodeURL = %q, expected it to contain %q", got, want)
		}
	}
}

func TestHealthCheck(t *testing.T) {
	const wellKnown = "/.well-known/openid-configuration"

	t.Run("reachable provider is healthy", func(t *testing.T) {
		var gotPath string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotPath = r.URL.Path
			w.WriteHeader(http.StatusOK)
		}))
		defer srv.Close()

		s := &AuthService{provider: srv.URL, httpClient: srv.Client()}
		if err := s.HealthCheck(context.Background()); err != nil {
			t.Fatalf("HealthCheck: %v", err)
		}
		if gotPath != wellKnown {
			t.Errorf("requested path = %q, want %q", gotPath, wellKnown)
		}
	})

	t.Run("5xx from provider is unhealthy", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer srv.Close()

		s := &AuthService{provider: srv.URL, httpClient: srv.Client()}
		if err := s.HealthCheck(context.Background()); err == nil {
			t.Fatal("expected an error when the provider returns 500")
		}
	})

	t.Run("unreachable provider is unhealthy", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
		s := &AuthService{provider: srv.URL, httpClient: srv.Client()}
		srv.Close() // now refuses connections

		if err := s.HealthCheck(context.Background()); err == nil {
			t.Fatal("expected an error when the provider is unreachable")
		}
	})
}
