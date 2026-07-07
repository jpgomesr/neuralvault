package config

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// resetGlobals resets package-level state between tests.
//
// The config package uses singletons (sync.Once), so tests must
// clear shared state to remain independent.
func resetGlobals() {
	cfg = nil
	err = nil
	once = sync.Once{}
	validate = nil
	validateOnce = sync.Once{}
}

// setValidEnv populates a complete set of valid environment
// variables used as a baseline by multiple tests.
func setValidEnv(t *testing.T) {
	t.Helper()

	t.Setenv("SERVER_ENV", "development")
	t.Setenv("SERVER_PORT", "8080")

	t.Setenv("POSTGRES_HOST", "localhost")
	t.Setenv("POSTGRES_PORT", "5432")
	t.Setenv("POSTGRES_USERNAME", "user")
	t.Setenv("POSTGRES_PASSWORD", "pass")
	t.Setenv("POSTGRES_NAME", "db")
	t.Setenv("POSTGRES_SSL_MODE", "disable")

	t.Setenv("QDRANT_PORT", "6333")
	t.Setenv("QDRANT_GRPC_PORT", "6334")
	t.Setenv("QDRANT_URL", "http://localhost:6333")
	t.Setenv("QDRANT_API_KEY", "test-key")
	t.Setenv("QDRANT_COLLECTION_NAME", "neuralvault")
	t.Setenv("QDRANT_VECTOR_SIZE", "768")

	t.Setenv("OLLAMA_PORT", "11434")
	t.Setenv("OLLAMA_URL", "http://localhost:11434")
	t.Setenv("OLLAMA_EMBEDDING_MODEL", "nomic-embed-text")
	t.Setenv("OLLAMA_COMPLETION_MODEL", "llama3")

	t.Setenv("MINIO_ENDPOINT", "localhost:9000")
	t.Setenv("MINIO_ACCESS_KEY", "minioadmin")
	t.Setenv("MINIO_SECRET_KEY", "minioadmin")
	t.Setenv("MINIO_BUCKET", "neuralvault")

	t.Setenv("AUTH_ISSUER_URL", "http://localhost:8081/realms/neuralvault")
	t.Setenv("AUTH_CLIENT_ID", "neuralvault")
	t.Setenv("AUTH_CLIENT_SECRET", "test-secret")
	t.Setenv("AUTH_REDIRECT_URL", "http://localhost:8080/auth/callback")
	t.Setenv("AUTH_SESSION_SECRET", "test-session-secret-at-least-32-bytes")
}

// Ensures the validator is initialized once.
func TestGetValidator_Singleton(t *testing.T) {
	resetGlobals()

	first := getValidator()
	second := getValidator()

	if first == nil || second == nil {
		t.Fatal("expected validator instance")
	}
	if first != second {
		t.Fatal("expected singleton validator instance")
	}
}

// Ensures loadEnvFile loads env vars from a file.
func TestLoadEnvFile_LoadsEnvFile(t *testing.T) {
	resetGlobals()

	configDir := t.TempDir()
	filePath := filepath.Join(configDir, ".env.test")

	if err := os.WriteFile(filePath, []byte("CONFIG_TEST_VAR=loaded\n"), 0644); err != nil {
		t.Fatalf("failed to write env file: %v", err)
	}

	if err := os.Unsetenv("CONFIG_TEST_VAR"); err != nil {
		t.Fatalf("failed to unset CONFIG_TEST_VAR: %v", err)
	}
	loadEnvFile(filePath)

	if got := os.Getenv("CONFIG_TEST_VAR"); got != "loaded" {
		t.Fatalf("expected CONFIG_TEST_VAR=loaded, got: %q", got)
	}
}

// Ensures missing env files do not mutate environment variables.
func TestLoadEnvFile_MissingFile(t *testing.T) {
	resetGlobals()

	configDir := t.TempDir()
	filePath := filepath.Join(configDir, ".env.missing")

	if err := os.Unsetenv("CONFIG_TEST_VAR"); err != nil {
		t.Fatalf("failed to unset CONFIG_TEST_VAR: %v", err)
	}
	loadEnvFile(filePath)

	if got := os.Getenv("CONFIG_TEST_VAR"); got != "" {
		t.Fatalf("expected CONFIG_TEST_VAR to be unset, got: %q", got)
	}
}

// Verifies that configuration is loaded only once and
// subsequent calls return the same cached instance.
func TestLoad_Singleton(t *testing.T) {
	resetGlobals()

	configDir := t.TempDir()
	t.Setenv("CONFIG_DIR", configDir)

	setValidEnv(t)

	cfg1, err := Load()
	if err != nil {
		t.Fatal(err)
	}

	t.Setenv("SERVER_PORT", "9999")

	cfg2, err := Load()
	if err != nil {
		t.Fatal(err)
	}

	if cfg1 != cfg2 {
		t.Fatal("expected same config instance")
	}

	if cfg2.Server.Port != 8080 {
		t.Fatal("singleton should not reload configuration")
	}
}

// Ensures that .env files are ignored when running in production
// and only system environment variables are used.
func TestLoadConfig_ProductionSkipsDotEnv(t *testing.T) {
	resetGlobals()

	configDir := t.TempDir()
	t.Setenv("CONFIG_DIR", configDir)
	t.Setenv("SERVER_ENV", "production")

	setValidEnv(t)

	err := os.WriteFile(
		filepath.Join(configDir, ".env"),
		[]byte("SERVER_PORT=9999"),
		0644,
	)
	if err != nil {
		t.Fatal(err)
	}

	cfg, err := loadConfig()
	if err != nil {
		t.Fatal(err)
	}

	if cfg.Server.Port != 8080 {
		t.Fatal(".env should not be loaded in production")
	}
}

// Ensures validation fails when required configuration
// values are not provided.
func TestLoadConfig_MissingRequiredVariables(t *testing.T) {
	resetGlobals()

	configDir := t.TempDir()
	t.Setenv("CONFIG_DIR", configDir)

	t.Setenv("SERVER_ENV", "")
	t.Setenv("SERVER_PORT", "")
	t.Setenv("POSTGRES_HOST", "")
	t.Setenv("POSTGRES_PORT", "")
	t.Setenv("POSTGRES_USERNAME", "")
	t.Setenv("POSTGRES_PASSWORD", "")
	t.Setenv("POSTGRES_NAME", "")
	t.Setenv("POSTGRES_SSL_MODE", "")

	_, err := loadConfig()
	if err == nil {
		t.Fatalf("expected validation error, got nil")
	}
	if !strings.Contains(err.Error(), "error loading config") {
		t.Fatalf("expected config loading error, got: %v", err)
	}
}

// Ensures port validation rejects values outside the
// valid TCP/UDP port range.
func TestLoadConfig_InvalidPort(t *testing.T) {
	resetGlobals()

	configDir := t.TempDir()
	t.Setenv("CONFIG_DIR", configDir)

	setValidEnv(t)
	t.Setenv("SERVER_PORT", "70000")

	_, err := loadConfig()
	if err == nil {
		t.Fatalf("expected validation error, got nil")
	}
	if !strings.Contains(err.Error(), "Server.Port") {
		t.Fatalf("expected Server.Port validation error, got: %v", err)
	}
}

// Ensures Postgres port validation rejects values outside the
// valid TCP/UDP port range.
func TestLoadConfig_InvalidPostgresPort(t *testing.T) {
	resetGlobals()

	configDir := t.TempDir()
	t.Setenv("CONFIG_DIR", configDir)

	setValidEnv(t)
	t.Setenv("POSTGRES_PORT", "70000")

	_, err := loadConfig()
	if err == nil {
		t.Fatalf("expected validation error, got nil")
	}
	if !strings.Contains(err.Error(), "Postgres.Port") {
		t.Fatalf("expected Postgres.Port validation error, got: %v", err)
	}
}

// Ensures Qdrant port validation rejects values outside the
// valid TCP/UDP port range.
func TestLoadConfig_InvalidQdrantPort(t *testing.T) {
	resetGlobals()

	configDir := t.TempDir()
	t.Setenv("CONFIG_DIR", configDir)

	setValidEnv(t)
	t.Setenv("QDRANT_PORT", "70000")

	_, err := loadConfig()
	if err == nil {
		t.Fatalf("expected validation error, got nil")
	}
	if !strings.Contains(err.Error(), "Qdrant.Port") {
		t.Fatalf("expected Qdrant.Port validation error, got: %v", err)
	}
}

// Ensures Qdrant gRPC port validation rejects values outside the
// valid TCP/UDP port range.
func TestLoadConfig_InvalidQdrantGrpcPort(t *testing.T) {
	resetGlobals()

	configDir := t.TempDir()
	t.Setenv("CONFIG_DIR", configDir)

	setValidEnv(t)
	t.Setenv("QDRANT_GRPC_PORT", "70000")

	_, err := loadConfig()
	if err == nil {
		t.Fatalf("expected validation error, got nil")
	}
	if !strings.Contains(err.Error(), "Qdrant.GrpcPort") {
		t.Fatalf("expected Qdrant.GrpcPort validation error, got: %v", err)
	}
}

// Ensures Ollama port validation rejects values outside the
// valid TCP/UDP port range.
func TestLoadConfig_InvalidOllamaPort(t *testing.T) {
	resetGlobals()

	configDir := t.TempDir()
	t.Setenv("CONFIG_DIR", configDir)

	setValidEnv(t)
	t.Setenv("OLLAMA_PORT", "70000")

	_, err := loadConfig()
	if err == nil {
		t.Fatalf("expected validation error, got nil")
	}
	if !strings.Contains(err.Error(), "Ollama.Port") {
		t.Fatalf("expected Ollama.Port validation error, got: %v", err)
	}
}

// Ensures SSL mode validation only accepts supported
// PostgreSQL SSL modes.
func TestLoadConfig_InvalidSSLMode(t *testing.T) {
	resetGlobals()

	configDir := t.TempDir()
	t.Setenv("CONFIG_DIR", configDir)

	setValidEnv(t)
	t.Setenv("POSTGRES_SSL_MODE", "invalid")

	_, err := loadConfig()
	if err == nil {
		t.Fatalf("expected validation error, got nil")
	}
	if !strings.Contains(err.Error(), "Postgres.SSLMode") {
		t.Fatalf("expected Postgres.SSLMode validation error, got: %v", err)
	}
}

// Ensures the session secret is rejected when shorter than the
// minimum length required to sign the session cookie securely.
func TestLoadConfig_ShortSessionSecret(t *testing.T) {
	resetGlobals()

	configDir := t.TempDir()
	t.Setenv("CONFIG_DIR", configDir)

	setValidEnv(t)
	t.Setenv("AUTH_SESSION_SECRET", "too-short")

	_, err := loadConfig()
	if err == nil {
		t.Fatalf("expected validation error, got nil")
	}
	if !strings.Contains(err.Error(), "Auth.SessionSecret") {
		t.Fatalf("expected Auth.SessionSecret validation error, got: %v", err)
	}
}

// Verifies that a valid environment produces a fully
// populated configuration object.
func TestLoadConfig_SuccessfulLoad(t *testing.T) {
	resetGlobals()

	configDir := t.TempDir()
	t.Setenv("CONFIG_DIR", configDir)

	setValidEnv(t)
	t.Setenv("SERVER_PORT", "9090")

	cfg, err := loadConfig()
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if cfg.Server.Port != 9090 {
		t.Fatalf("expected server port 9090, got: %d", cfg.Server.Port)
	}
	if cfg.Server.Env != "development" {
		t.Fatalf("expected server env development, got: %s", cfg.Server.Env)
	}
	if cfg.Postgres.Host != "localhost" {
		t.Fatalf("expected postgres host localhost, got: %s", cfg.Postgres.Host)
	}
	if cfg.Qdrant.Port != 6333 {
		t.Fatalf("expected qdrant port 6333, got: %d", cfg.Qdrant.Port)
	}
	if cfg.Ollama.EmbeddingModel != "nomic-embed-text" {
		t.Fatalf("expected embedding model nomic-embed-text, got: %s", cfg.Ollama.EmbeddingModel)
	}
	if cfg.Ollama.CompletionModel != "llama3" {
		t.Fatalf("expected completion model llama3, got: %s", cfg.Ollama.CompletionModel)
	}

	dsn := cfg.Postgres.DSN()
	if !strings.Contains(dsn, "host=localhost") || !strings.Contains(dsn, "dbname=db") {
		t.Fatalf("expected DSN to include host and dbname, got: %s", dsn)
	}
}

// Ensures the HTTP-hardening server settings apply their defaults and can be
// overridden via environment variables.
func TestLoadConfig_ServerHardeningDefaultsAndOverrides(t *testing.T) {
	resetGlobals()

	configDir := t.TempDir()
	t.Setenv("CONFIG_DIR", configDir)
	setValidEnv(t)

	// Defaults: none of the SERVER_*_TIMEOUT / SERVER_MAX_UPLOAD_BYTES are set.
	cfg, err := loadConfig()
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	defaults := []struct {
		name string
		got  time.Duration
		want time.Duration
	}{
		{"ReadHeaderTimeout", cfg.Server.ReadHeaderTimeout, 10 * time.Second},
		{"ReadTimeout", cfg.Server.ReadTimeout, 60 * time.Second},
		{"WriteTimeout", cfg.Server.WriteTimeout, 120 * time.Second},
		{"IdleTimeout", cfg.Server.IdleTimeout, 120 * time.Second},
		{"ShutdownTimeout", cfg.Server.ShutdownTimeout, 20 * time.Second},
	}
	for _, d := range defaults {
		if d.got != d.want {
			t.Errorf("expected %s default %v, got %v", d.name, d.want, d.got)
		}
	}
	if cfg.Server.MaxUploadBytes != 104857600 {
		t.Errorf("expected MaxUploadBytes default 104857600, got %d", cfg.Server.MaxUploadBytes)
	}

	// Overrides via environment.
	resetGlobals()
	t.Setenv("SERVER_READ_HEADER_TIMEOUT", "5s")
	t.Setenv("SERVER_WRITE_TIMEOUT", "1m")
	t.Setenv("SERVER_MAX_UPLOAD_BYTES", "1024")

	cfg, err = loadConfig()
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if cfg.Server.ReadHeaderTimeout != 5*time.Second {
		t.Errorf("expected ReadHeaderTimeout 5s, got %v", cfg.Server.ReadHeaderTimeout)
	}
	if cfg.Server.WriteTimeout != time.Minute {
		t.Errorf("expected WriteTimeout 1m, got %v", cfg.Server.WriteTimeout)
	}
	if cfg.Server.MaxUploadBytes != 1024 {
		t.Errorf("expected MaxUploadBytes 1024, got %d", cfg.Server.MaxUploadBytes)
	}
}

// Ensures each required string field is validated when missing.
func TestLoadConfig_MissingRequiredStringFields(t *testing.T) {
	required := []struct {
		name      string
		envVar    string
		fieldName string
	}{
		{name: "server env", envVar: "SERVER_ENV", fieldName: "Server.Env"},
		{name: "postgres host", envVar: "POSTGRES_HOST", fieldName: "Postgres.Host"},
		// NOTE: this case reliably fails on Windows. envconfig falls back to a
		// field's bare `envconfig` tag ("USERNAME") when the prefixed key
		// ("POSTGRES_USERNAME") is unset, and Windows always defines a
		// built-in USERNAME env var (the logged-in account name), so the
		// fallback resolves to a non-empty value and validation passes
		// instead of failing. See kelseyhightower/envconfig's Process(),
		// the `info.Alt` lookup. Not something this repo's config code
		// causes or can special-case away.
		{name: "postgres username", envVar: "POSTGRES_USERNAME", fieldName: "Postgres.Username"},
		{name: "postgres password", envVar: "POSTGRES_PASSWORD", fieldName: "Postgres.Password"},
		{name: "postgres name", envVar: "POSTGRES_NAME", fieldName: "Postgres.Name"},
		{name: "qdrant url", envVar: "QDRANT_URL", fieldName: "Qdrant.URL"},
		{name: "qdrant api key", envVar: "QDRANT_API_KEY", fieldName: "Qdrant.APIKey"},
		{name: "qdrant collection name", envVar: "QDRANT_COLLECTION_NAME", fieldName: "Qdrant.CollectionName"},
		{name: "ollama url", envVar: "OLLAMA_URL", fieldName: "Ollama.URL"},
		{name: "ollama embedding model", envVar: "OLLAMA_EMBEDDING_MODEL", fieldName: "Ollama.EmbeddingModel"},
		{name: "ollama completion model", envVar: "OLLAMA_COMPLETION_MODEL", fieldName: "Ollama.CompletionModel"},
		{name: "minio endpoint", envVar: "MINIO_ENDPOINT", fieldName: "MinIO.Endpoint"},
		{name: "minio access key", envVar: "MINIO_ACCESS_KEY", fieldName: "MinIO.AccessKey"},
		{name: "minio secret key", envVar: "MINIO_SECRET_KEY", fieldName: "MinIO.SecretKey"},
		{name: "minio bucket", envVar: "MINIO_BUCKET", fieldName: "MinIO.Bucket"},
		{name: "auth issuer url", envVar: "AUTH_ISSUER_URL", fieldName: "Auth.IssuerURL"},
		{name: "auth client id", envVar: "AUTH_CLIENT_ID", fieldName: "Auth.ClientID"},
		{name: "auth client secret", envVar: "AUTH_CLIENT_SECRET", fieldName: "Auth.ClientSecret"},
		{name: "auth redirect url", envVar: "AUTH_REDIRECT_URL", fieldName: "Auth.RedirectURL"},
		{name: "auth session secret", envVar: "AUTH_SESSION_SECRET", fieldName: "Auth.SessionSecret"},
	}

	for _, tc := range required {
		t.Run(tc.name, func(t *testing.T) {
			resetGlobals()

			configDir := t.TempDir()
			t.Setenv("CONFIG_DIR", configDir)

			setValidEnv(t)
			if err := os.Unsetenv(tc.envVar); err != nil {
				t.Fatalf("failed to unset %s: %v", tc.envVar, err)
			}

			_, err := loadConfig()
			if err == nil {
				t.Fatalf("expected validation error, got nil")
			}
			if !strings.Contains(err.Error(), "validation errors") {
				t.Fatalf("expected validation errors, got: %v", err)
			}
			if !strings.Contains(err.Error(), tc.fieldName) {
				t.Fatalf("expected validation error for %s, got: %v", tc.fieldName, err)
			}
		})
	}
}

// Ensures the package-level DSN helper returns a valid postgres:// URL
// that includes the host and database name from the loaded config.
func TestDSN_ReturnsURL(t *testing.T) {
	resetGlobals()

	configDir := t.TempDir()
	t.Setenv("CONFIG_DIR", configDir)
	setValidEnv(t)

	if _, err := Load(); err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	dsn := DSN()
	if !strings.HasPrefix(dsn, "postgres://") {
		t.Fatalf("expected postgres:// scheme, got: %s", dsn)
	}
	if !strings.Contains(dsn, "localhost") {
		t.Fatalf("expected localhost in DSN, got: %s", dsn)
	}
	if !strings.Contains(dsn, "/db") {
		t.Fatalf("expected /db in DSN, got: %s", dsn)
	}
}

// Ensures environment-specific .env files override values
// from the base .env file.
func TestLoadConfig_DotEnvLoading(t *testing.T) {
	resetGlobals()

	configDir := t.TempDir()
	t.Setenv("CONFIG_DIR", configDir)
	t.Setenv("SERVER_ENV", "development")

	for _, key := range []string{
		"SERVER_PORT",
		"POSTGRES_HOST",
		"POSTGRES_PORT",
		"POSTGRES_USERNAME",
		"POSTGRES_PASSWORD",
		"POSTGRES_NAME",
		"POSTGRES_SSL_MODE",
		"QDRANT_PORT",
		"QDRANT_GRPC_PORT",
		"QDRANT_URL",
		"QDRANT_API_KEY",
		"QDRANT_COLLECTION_NAME",
		"QDRANT_VECTOR_SIZE",
		"OLLAMA_PORT",
		"OLLAMA_URL",
		"OLLAMA_EMBEDDING_MODEL",
		"OLLAMA_COMPLETION_MODEL",
		"MINIO_ENDPOINT",
		"MINIO_ACCESS_KEY",
		"MINIO_SECRET_KEY",
		"MINIO_BUCKET",
		"AUTH_ISSUER_URL",
		"AUTH_CLIENT_ID",
		"AUTH_CLIENT_SECRET",
		"AUTH_REDIRECT_URL",
		"AUTH_SESSION_SECRET",
	} {
		if err := os.Unsetenv(key); err != nil {
			t.Fatalf("failed to unset %s: %v", key, err)
		}
	}

	envBase := strings.Join([]string{
		"SERVER_ENV=development",
		"SERVER_PORT=8181",
		"POSTGRES_HOST=basehost",
		"POSTGRES_PORT=5432",
		"POSTGRES_USERNAME=baseuser",
		"POSTGRES_NAME=basedb",
		"POSTGRES_SSL_MODE=disable",
		"QDRANT_PORT=6333",
		"QDRANT_GRPC_PORT=6334",
		"QDRANT_URL=http://localhost:6333",
		"QDRANT_API_KEY=test-key",
		"QDRANT_COLLECTION_NAME=neuralvault",
		"QDRANT_VECTOR_SIZE=768",
		"OLLAMA_PORT=11434",
		"OLLAMA_URL=http://localhost:11434",
		"OLLAMA_EMBEDDING_MODEL=nomic-embed-text",
		"OLLAMA_COMPLETION_MODEL=llama3",
		"MINIO_ENDPOINT=localhost:9000",
		"MINIO_ACCESS_KEY=minioadmin",
		"MINIO_SECRET_KEY=minioadmin",
		"MINIO_BUCKET=neuralvault",
		"AUTH_ISSUER_URL=http://localhost:8081/realms/neuralvault",
		"AUTH_CLIENT_ID=neuralvault",
		"AUTH_CLIENT_SECRET=test-secret",
		"AUTH_REDIRECT_URL=http://localhost:8080/auth/callback",
		"AUTH_SESSION_SECRET=test-session-secret-at-least-32-bytes",
	}, "\n") + "\n"

	envDev := "POSTGRES_PASSWORD=devpass\n"

	if err := os.WriteFile(filepath.Join(configDir, ".env"), []byte(envBase), 0644); err != nil {
		t.Fatalf("failed to write .env: %v", err)
	}
	if err := os.WriteFile(filepath.Join(configDir, ".env.development"), []byte(envDev), 0644); err != nil {
		t.Fatalf("failed to write .env.development: %v", err)
	}

	cfg, err := loadConfig()
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if cfg.Postgres.Password != "devpass" {
		t.Fatalf("expected password from .env.development, got: %s", cfg.Postgres.Password)
	}
	if cfg.Postgres.Host != "basehost" {
		t.Fatalf("expected host from .env, got: %s", cfg.Postgres.Host)
	}
}
