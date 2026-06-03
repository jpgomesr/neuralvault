package config

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
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

	t.Setenv("QDGRANT_PORT", "6333")
	t.Setenv("QDGRANT_GRPC_PORT", "6334")
	t.Setenv("QDGRANT_URL", "http://localhost:6333")

	t.Setenv("OLLAMA_PORT", "11434")
	t.Setenv("OLLAMA_URL", "http://localhost:11434")
	t.Setenv("OLLAMA_EMBEDDING_MODEL", "nomic-embed-text")
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
	t.Setenv("QDGRANT_PORT", "70000")

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
	t.Setenv("QDGRANT_GRPC_PORT", "70000")

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

	dsn := cfg.Postgres.DSN()
	if !strings.Contains(dsn, "host=localhost") || !strings.Contains(dsn, "dbname=db") {
		t.Fatalf("expected DSN to include host and dbname, got: %s", dsn)
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
		{name: "postgres username", envVar: "POSTGRES_USERNAME", fieldName: "Postgres.Username"},
		{name: "postgres password", envVar: "POSTGRES_PASSWORD", fieldName: "Postgres.Password"},
		{name: "postgres name", envVar: "POSTGRES_NAME", fieldName: "Postgres.Name"},
		{name: "qdrant url", envVar: "QDGRANT_URL", fieldName: "Qdrant.URL"},
		{name: "ollama url", envVar: "OLLAMA_URL", fieldName: "Ollama.URL"},
		{name: "ollama embedding model", envVar: "OLLAMA_EMBEDDING_MODEL", fieldName: "Ollama.EmbeddingModel"},
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
		"QDGRANT_PORT",
		"QDGRANT_GRPC_PORT",
		"QDGRANT_URL",
		"OLLAMA_PORT",
		"OLLAMA_URL",
		"OLLAMA_EMBEDDING_MODEL",
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
		"QDGRANT_PORT=6333",
		"QDGRANT_GRPC_PORT=6334",
		"QDGRANT_URL=http://localhost:6333",
		"OLLAMA_PORT=11434",
		"OLLAMA_URL=http://localhost:11434",
		"OLLAMA_EMBEDDING_MODEL=nomic-embed-text",
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
