package config

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"

	"github.com/go-playground/validator"
	"github.com/joho/godotenv"
	"github.com/kelseyhightower/envconfig"
)

// Config contains all application configuration loaded from
// environment variables and validated during startup.
type Config struct {
	Server   Server   `envconfig:"SERVER"`
	Postgres Postgres `envconfig:"POSTGRES"`
	Qdrant   Qdrant   `envconfig:"QDRANT"`
	Ollama   Ollama   `envconfig:"OLLAMA"`
}

// Server contains HTTP server configuration.
type Server struct {
	Port int    `envconfig:"PORT" default:"8180" validate:"gte=1,lte=65535"`
	Env  string `envconfig:"ENV" validate:"required"`
}

// Postgres contains database connection settings.
type Postgres struct {
	Host     string `envconfig:"HOST" validate:"required"`
	Port     int    `envconfig:"PORT" default:"5432" validate:"gte=1,lte=65535"`
	Username string `envconfig:"USERNAME" validate:"required"`
	Password string `envconfig:"PASSWORD" validate:"required"`
	Name     string `envconfig:"NAME" validate:"required"`
	SSLMode  string `envconfig:"SSL_MODE" default:"disable" validate:"oneof=disable allow prefer require verify-ca verify-full"`
	MaxConns int    `envconfig:"MAXCONNS" default:"10" validate:"gte=1,lte=65535"`
	MinConns int    `envconfig:"MINCONNS" default:"0" validate:"gte=1,lte=65535"`
}

// Qdrant contains vector database connection settings.
type Qdrant struct {
	Port     int    `envconfig:"PORT" validate:"required,gte=1,lte=65535"`
	GrpcPort int    `envconfig:"GRPC_PORT" validate:"required,gte=1,lte=65535"`
	URL      string `envconfig:"URL" validate:"required"`
}

// Ollama contains local-LLM configuration.
type Ollama struct {
	Port           int    `envconfig:"PORT" validate:"required,gte=1,lte=65535"`
	URL            string `envconfig:"URL" validate:"required"`
	EmbeddingModel string `envconfig:"EMBEDDING_MODEL" validate:"required"`
}

var (
	cfg  *Config
	once sync.Once
	err  error
)

// Load loads application configuration once and returns the same
// instance for all subsequent calls.
//
// Configuration is loaded from environment variables, validated,
// and cached using sync.Once.
func Load() (*Config, error) {
	once.Do(func() {
		cfg, err = loadConfig()
		if err != nil {
			slog.Error("error loading config", "error", err)
		}
	})

	return cfg, err
}

// loadConfig performs the actual configuration loading and validation.
func loadConfig() (*Config, error) {
	var c Config

	loadDotEnvFiles()

	if err := envconfig.Process("", &c); err != nil {
		return nil, fmt.Errorf("error loading config: %w", err)
	}

	if err := validateConfig(&c); err != nil {
		return nil, fmt.Errorf("error validating config: %w", err)
	}

	slog.Info("config loaded", "env", c.Server.Env)

	return &c, nil
}

// loadDotEnvFiles loads configuration files for local development.
//
// Loading order:
//   - .env
//   - .env.<environment>
//
// Environment-specific values override values from .env.
//
// In production, only system environment variables are used.
func loadDotEnvFiles() {
	env := os.Getenv("SERVER_ENV")
	if env == "" {
		env = "development"
	}

	baseDir := os.Getenv("CONFIG_DIR")
	if baseDir == "" {
		wd, err := os.Getwd()
		if err == nil {
			baseDir = wd
		} else {
			baseDir = "."
		}
	}

	if env == "production" {
		return
	}

	loadEnvFile(filepath.Join(baseDir, ".env"))
	loadEnvFile(filepath.Join(baseDir, ".env."+env))
}

// loadEnvFile loads a single .env file if it exists.
func loadEnvFile(filename string) {
	err := godotenv.Load(filename)
	if err == nil {
		return
	}

	if os.IsNotExist(err) {
		slog.Debug("config file not found", "filename", filename)
	} else {
		slog.Error("error loading config file",
			"filename", filename,
			"error", err,
		)
	}
}

// DSN returns a PostgreSQL connection string compatible with pgx/libpq.
func (p Postgres) DSN() string {
	return fmt.Sprintf(
		"host=%s port=%d user=%s password=%q dbname=%s sslmode=%s",
		p.Host,
		p.Port,
		p.Username,
		p.Password,
		p.Name,
		p.SSLMode,
	)
}

var (
	validate     *validator.Validate
	validateOnce sync.Once
)

// getValidator lazily initializes the validator instance.
func getValidator() *validator.Validate {
	validateOnce.Do(func() {
		validate = validator.New()
	})

	return validate
}

// validateConfig validates all configuration structs using
// the tags defined on the fields.
func validateConfig(cfg *Config) error {
	err := getValidator().Struct(cfg)
	if err == nil {
		return nil
	}

	var ve validator.ValidationErrors
	if !errors.As(err, &ve) {
		return fmt.Errorf("unexpected validation error: %w", err)
	}

	var errs []string

	for _, fe := range ve {
		errs = append(
			errs,
			fmt.Sprintf(
				"field '%s' with value '%v' failed on rule '%s'",
				fe.Namespace(),
				fe.Value(),
				fe.Tag(),
			),
		)
	}

	return fmt.Errorf("validation errors: %v", errs)
}

// DSN returns the structured DSN config
func DSN() string {
	return fmt.Sprintf(
		"postgres://%s:%s@%s:%d/%s?sslmode=%s",
		cfg.Postgres.Username, cfg.Postgres.Password, cfg.Postgres.Host, cfg.Postgres.Port, cfg.Postgres.Name, cfg.Postgres.SSLMode,
	)
}
