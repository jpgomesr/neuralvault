package modelconfig

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/jpgomesr/NeuralVault/internal/catalog"
	"github.com/jpgomesr/NeuralVault/internal/config"
	"github.com/jpgomesr/NeuralVault/internal/crypto"
	pgstore "github.com/jpgomesr/NeuralVault/internal/storage/postgres"
)

// testEncryptionKey is a throwaway base64-encoded 32-byte AES-256 key, the
// same shape as SECRETS_ENCRYPTION_KEY — not a secret, since it only ever
// protects data inside an ephemeral test container.
const testEncryptionKey = "ZGV2LW9ubHkta2V5LWRvLW5vdC11c2UtaW4tcHJvZCE="

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

// insertWorkspace inserts a workspace row and schedules its deletion on test
// cleanup. Deleting the workspace cascades to provider_credential and
// workspace_model_settings rows, so tests don't need separate teardown.
func insertWorkspace(ctx context.Context, t *testing.T) uuid.UUID {
	t.Helper()
	id := uuid.New()
	if _, err := sharedPool.Exec(ctx, "INSERT INTO workspace (id, name) VALUES ($1, $2)", id, "test"); err != nil {
		t.Fatalf("insert workspace: %v", err)
	}
	t.Cleanup(func() {
		_, _ = sharedPool.Exec(context.Background(), "DELETE FROM workspace WHERE id = $1", id)
	})
	return id
}

func newTestStore(t *testing.T) *store {
	t.Helper()
	cipher, err := crypto.New(testEncryptionKey)
	if err != nil {
		t.Fatalf("crypto.New: %v", err)
	}
	return newStore(sharedPool, cipher)
}

func TestHint(t *testing.T) {
	tests := []struct {
		name string
		key  string
		want string
	}{
		{name: "empty key", key: "", want: ""},
		{name: "key shorter than hint length", key: "abc", want: ""},
		{name: "key exactly hint length", key: "abcd", want: ""},
		{name: "typical key", key: "sk-ant-api03-abcdWXYZ", want: "WXYZ"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := hint(tt.key); got != tt.want {
				t.Errorf("hint(%q) = %q, want %q", tt.key, got, tt.want)
			}
		})
	}
}

func TestStore_SaveAndGetCredential_RoundTrip(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	wsID := insertWorkspace(ctx, t)

	if err := s.SaveCredential(ctx, wsID, catalog.Anthropic, "sk-test-key", "https://example.com"); err != nil {
		t.Fatalf("SaveCredential: %v", err)
	}

	apiKey, baseURL, err := s.GetCredential(ctx, wsID, catalog.Anthropic)
	if err != nil {
		t.Fatalf("GetCredential: %v", err)
	}
	if apiKey != "sk-test-key" {
		t.Errorf("apiKey = %q, want %q", apiKey, "sk-test-key")
	}
	if baseURL != "https://example.com" {
		t.Errorf("baseURL = %q, want %q", baseURL, "https://example.com")
	}
}

func TestStore_SaveCredential_EmptyBaseURLFallsBackToNull(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	wsID := insertWorkspace(ctx, t)

	if err := s.SaveCredential(ctx, wsID, catalog.OpenAI, "sk-test-key", ""); err != nil {
		t.Fatalf("SaveCredential: %v", err)
	}

	_, baseURL, err := s.GetCredential(ctx, wsID, catalog.OpenAI)
	if err != nil {
		t.Fatalf("GetCredential: %v", err)
	}
	if baseURL != "" {
		t.Errorf("baseURL = %q, want empty", baseURL)
	}
}

func TestStore_GetCredential_NotFound(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	wsID := insertWorkspace(ctx, t)

	_, _, err := s.GetCredential(ctx, wsID, catalog.Anthropic)
	if !errors.Is(err, ErrCredentialNotFound) {
		t.Fatalf("err = %v, want ErrCredentialNotFound", err)
	}
}

func TestStore_SaveCredential_UpsertOverwritesPreviousKey(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	wsID := insertWorkspace(ctx, t)

	if err := s.SaveCredential(ctx, wsID, catalog.Anthropic, "sk-old", ""); err != nil {
		t.Fatalf("SaveCredential (old): %v", err)
	}
	if err := s.SaveCredential(ctx, wsID, catalog.Anthropic, "sk-new", ""); err != nil {
		t.Fatalf("SaveCredential (new): %v", err)
	}

	apiKey, _, err := s.GetCredential(ctx, wsID, catalog.Anthropic)
	if err != nil {
		t.Fatalf("GetCredential: %v", err)
	}
	if apiKey != "sk-new" {
		t.Errorf("apiKey = %q, want %q (the upsert should overwrite, not duplicate)", apiKey, "sk-new")
	}
}

func TestStore_ListCredentials_NeverReturnsTheKey(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	wsID := insertWorkspace(ctx, t)

	if err := s.SaveCredential(ctx, wsID, catalog.Anthropic, "sk-anthropic-secret", ""); err != nil {
		t.Fatalf("SaveCredential: %v", err)
	}
	if err := s.SaveCredential(ctx, wsID, catalog.OpenAI, "sk-openai-secret", ""); err != nil {
		t.Fatalf("SaveCredential: %v", err)
	}

	creds, err := s.ListCredentials(ctx, wsID)
	if err != nil {
		t.Fatalf("ListCredentials: %v", err)
	}
	if len(creds) != 2 {
		t.Fatalf("len(creds) = %d, want 2", len(creds))
	}
	for _, c := range creds {
		if c.APIKeyHint != "cret" {
			t.Errorf("provider %q: hint = %q, want %q", c.Provider, c.APIKeyHint, "cret")
		}
	}
}

func TestStore_DeleteCredential(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	wsID := insertWorkspace(ctx, t)

	if err := s.SaveCredential(ctx, wsID, catalog.Anthropic, "sk-test", ""); err != nil {
		t.Fatalf("SaveCredential: %v", err)
	}
	if err := s.DeleteCredential(ctx, wsID, catalog.Anthropic); err != nil {
		t.Fatalf("DeleteCredential: %v", err)
	}

	_, _, err := s.GetCredential(ctx, wsID, catalog.Anthropic)
	if !errors.Is(err, ErrCredentialNotFound) {
		t.Fatalf("err = %v, want ErrCredentialNotFound after delete", err)
	}
}

// TestStore_DeleteCredential_NonExistentIsNotAnError matches the doc comment
// on store.DeleteCredential: deleting a credential that was never saved must
// not surface as a failure to the settings UI.
func TestStore_DeleteCredential_NonExistentIsNotAnError(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	wsID := insertWorkspace(ctx, t)

	if err := s.DeleteCredential(ctx, wsID, catalog.Anthropic); err != nil {
		t.Fatalf("DeleteCredential: %v", err)
	}
}

// TestStore_GetSettings_NoRow matches the doc comment on store.GetSettings: a
// workspace that never configured anything gets zero-valued settings, not an
// error — that's the normal "use the server defaults" state.
func TestStore_GetSettings_NoRow(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	wsID := insertWorkspace(ctx, t)

	settings, err := s.GetSettings(ctx, wsID)
	if err != nil {
		t.Fatalf("GetSettings: %v", err)
	}
	if settings.HasLLM() || settings.HasEmbedding() {
		t.Fatalf("settings = %+v, want no LLM or embedding configured", settings)
	}
}

func TestStore_SaveLLMSettings_RoundTrip(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	wsID := insertWorkspace(ctx, t)

	if err := s.SaveLLMSettings(ctx, wsID, catalog.Anthropic, "claude-sonnet-5"); err != nil {
		t.Fatalf("SaveLLMSettings: %v", err)
	}

	settings, err := s.GetSettings(ctx, wsID)
	if err != nil {
		t.Fatalf("GetSettings: %v", err)
	}
	if settings.LLMProvider != catalog.Anthropic || settings.LLMModel != "claude-sonnet-5" {
		t.Errorf("settings = %+v, want provider=%q model=%q", settings, catalog.Anthropic, "claude-sonnet-5")
	}
	if settings.HasEmbedding() {
		t.Errorf("settings = %+v, want no embedding configured", settings)
	}
}

func TestStore_SaveEmbeddingSettings_RoundTrip(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	wsID := insertWorkspace(ctx, t)

	if err := s.SaveEmbeddingSettings(ctx, wsID, catalog.OpenAI, "text-embedding-3-small", "nv_openai_text_embedding_3_small_1536", 1536); err != nil {
		t.Fatalf("SaveEmbeddingSettings: %v", err)
	}

	settings, err := s.GetSettings(ctx, wsID)
	if err != nil {
		t.Fatalf("GetSettings: %v", err)
	}
	if settings.EmbeddingProvider != catalog.OpenAI ||
		settings.EmbeddingModel != "text-embedding-3-small" ||
		settings.EmbeddingCollection != "nv_openai_text_embedding_3_small_1536" ||
		settings.EmbeddingDimensions != 1536 {
		t.Errorf("settings = %+v, unexpected embedding fields", settings)
	}
	if settings.HasLLM() {
		t.Errorf("settings = %+v, want no LLM configured", settings)
	}
}

// TestStore_LLMAndEmbeddingSettingsCoexist guards the upsert shape both
// SaveLLMSettings and SaveEmbeddingSettings rely on: each writes only its own
// half of the same (workspace_id-keyed) row, via ON CONFLICT DO UPDATE SET
// that names only its own columns — so setting one must never clobber a
// previously saved value for the other.
func TestStore_LLMAndEmbeddingSettingsCoexist(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	wsID := insertWorkspace(ctx, t)

	if err := s.SaveLLMSettings(ctx, wsID, catalog.Anthropic, "claude-sonnet-5"); err != nil {
		t.Fatalf("SaveLLMSettings: %v", err)
	}
	if err := s.SaveEmbeddingSettings(ctx, wsID, catalog.OpenAI, "text-embedding-3-small", "nv_openai_text_embedding_3_small_1536", 1536); err != nil {
		t.Fatalf("SaveEmbeddingSettings: %v", err)
	}

	settings, err := s.GetSettings(ctx, wsID)
	if err != nil {
		t.Fatalf("GetSettings: %v", err)
	}
	if settings.LLMProvider != catalog.Anthropic || settings.LLMModel != "claude-sonnet-5" {
		t.Errorf("SaveEmbeddingSettings clobbered the LLM settings: %+v", settings)
	}
	if settings.EmbeddingProvider != catalog.OpenAI || settings.EmbeddingModel != "text-embedding-3-small" {
		t.Errorf("embedding settings not saved: %+v", settings)
	}

	// And the reverse order: updating LLM settings again must not disturb the
	// embedding half just saved above.
	if err := s.SaveLLMSettings(ctx, wsID, catalog.Anthropic, "claude-haiku-5"); err != nil {
		t.Fatalf("SaveLLMSettings (update): %v", err)
	}
	settings, err = s.GetSettings(ctx, wsID)
	if err != nil {
		t.Fatalf("GetSettings: %v", err)
	}
	if settings.LLMModel != "claude-haiku-5" {
		t.Errorf("LLMModel = %q, want %q", settings.LLMModel, "claude-haiku-5")
	}
	if settings.EmbeddingModel != "text-embedding-3-small" {
		t.Errorf("SaveLLMSettings clobbered the embedding settings: %+v", settings)
	}
}
