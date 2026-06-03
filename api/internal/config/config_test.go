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

	dsn := cfg.Postgres.DSN()
	if !strings.Contains(dsn, "host=localhost") || !strings.Contains(dsn, "dbname=db") {
		t.Fatalf("expected DSN to include host and dbname, got: %s", dsn)
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
