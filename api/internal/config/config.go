package config

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

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
	MinIO    MinIO    `envconfig:"MINIO"`
	Auth     Auth     `envconfig:"AUTH"`
	Reranker Reranker `envconfig:"RERANKER"`
	Secrets  Secrets  `envconfig:"SECRETS"`
}

// Server contains HTTP server configuration.
type Server struct {
	Port int    `envconfig:"PORT" default:"8180" validate:"gte=1,lte=65535"`
	Env  string `envconfig:"ENV" validate:"required"`
	// ReadHeaderTimeout bounds how long the server waits for request headers,
	// the primary guard against slowloris-style slow-client attacks.
	ReadHeaderTimeout time.Duration `envconfig:"READ_HEADER_TIMEOUT" default:"10s"`
	// ReadTimeout bounds the time to read the entire request, including the body.
	ReadTimeout time.Duration `envconfig:"READ_TIMEOUT" default:"60s"`
	// WriteTimeout bounds the time to write the response. The SSE status stream
	// opts out of this via http.ResponseController so long-lived streams survive.
	WriteTimeout time.Duration `envconfig:"WRITE_TIMEOUT" default:"120s"`
	// IdleTimeout bounds how long an idle keep-alive connection is kept open.
	IdleTimeout time.Duration `envconfig:"IDLE_TIMEOUT" default:"120s"`
	// ShutdownTimeout bounds graceful shutdown before in-flight connections are
	// forcibly closed on SIGINT/SIGTERM.
	ShutdownTimeout time.Duration `envconfig:"SHUTDOWN_TIMEOUT" default:"20s"`
	// MaxUploadBytes caps the size of an upload request body (default 100 MiB).
	MaxUploadBytes int64 `envconfig:"MAX_UPLOAD_BYTES" default:"104857600"`
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
	MinConns int    `envconfig:"MINCONNS" default:"0" validate:"gte=0,lte=65535"`
}

// Qdrant contains vector database connection settings.
type Qdrant struct {
	Port           int    `envconfig:"PORT" validate:"required,gte=1,lte=65535"`
	GrpcPort       int    `envconfig:"GRPC_PORT" validate:"required,gte=1,lte=65535"`
	URL            string `envconfig:"URL" validate:"required"`
	UseTLS         bool   `envconfig:"USE_TLS" default:"false"`
	APIKey         string `envconfig:"API_KEY" validate:"required"`
	CollectionName string `envconfig:"COLLECTION_NAME" validate:"required"`
	VectorSize     uint64 `envconfig:"VECTOR_SIZE" validate:"required"`
}

// Ollama contains the server's default local-LLM configuration.
//
// URL is the switch: leaving it empty disables Ollama entirely, so the server
// boots without it and has no fallback provider. Every workspace must then
// configure its own provider (BYOK) — see internal/modelconfig. When URL is
// set, the other three fields become required, since a partially configured
// Ollama would fail on first use instead of at startup.
type Ollama struct {
	Port            int    `envconfig:"PORT" validate:"required_with=URL,omitempty,gte=1,lte=65535"`
	URL             string `envconfig:"URL"`
	EmbeddingModel  string `envconfig:"EMBEDDING_MODEL" validate:"required_with=URL"`
	CompletionModel string `envconfig:"COMPLETION_MODEL" validate:"required_with=URL"`
}

// Enabled reports whether the server has a default Ollama provider. When
// false, there is no fallback: every workspace must configure its own
// provider.
func (o Ollama) Enabled() bool {
	return o.URL != ""
}

// MinIO contains object storage configuration.
// Compatible with any S3-compatible provider (MinIO, AWS S3, Cloudflare R2).
type MinIO struct {
	Endpoint  string `envconfig:"ENDPOINT"   validate:"required"`
	AccessKey string `envconfig:"ACCESS_KEY"  validate:"required"`
	SecretKey string `envconfig:"SECRET_KEY"  validate:"required"`
	Bucket    string `envconfig:"BUCKET"      validate:"required"`
	UseSSL    bool   `envconfig:"USE_SSL"`
}

// Reranker contains cross-encoder reranking service configuration (Hugging
// Face Text Embeddings Inference). Required like every other provider: a
// missing/unreachable reranker fails server startup rather than silently
// degrading retrieval quality on every query.
type Reranker struct {
	URL   string `envconfig:"URL" validate:"required"`
	Model string `envconfig:"MODEL" validate:"required"`
}

// Secrets contains the master key used to encrypt secrets at rest, currently
// the per-workspace provider API keys behind BYOK (see internal/crypto).
type Secrets struct {
	// EncryptionKey is a base64-encoded 32-byte key (AES-256-GCM). The length
	// is validated as the *encoded* form: 32 raw bytes is always 44 base64
	// characters. Generate one with `openssl rand -base64 32`.
	//
	// Rotating this key makes every stored API key undecryptable; workspaces
	// must re-enter them.
	EncryptionKey string `envconfig:"ENCRYPTION_KEY" validate:"required,len=44"`
}

// Auth contains OpenID Connect (OIDC) configuration for the authorization-code
// login flow. The integration targets the standard OIDC discovery spec, so the
// provider is swappable (Keycloak in dev, Google/GitHub/Auth0/… in other
// environments) by changing these values alone — no code changes.
type Auth struct {
	// IssuerURL is the OIDC issuer used for discovery (e.g. a Keycloak realm URL).
	IssuerURL string `envconfig:"ISSUER_URL" validate:"required"`
	// ClientID and ClientSecret identify this application to the provider.
	ClientID     string `envconfig:"CLIENT_ID" validate:"required"`
	ClientSecret string `envconfig:"CLIENT_SECRET" validate:"required"`
	// RedirectURL is the provider callback registered for this client.
	RedirectURL string `envconfig:"REDIRECT_URL" validate:"required"`
	// SessionSecret signs the stateless session cookie (HMAC-SHA256).
	SessionSecret string `envconfig:"SESSION_SECRET" validate:"required,min=32"`
	// CookieSecure marks the session cookie Secure; enable behind HTTPS.
	CookieSecure bool `envconfig:"COOKIE_SECURE" default:"false"`
	// PostLoginURL is where the browser is redirected after a successful login.
	PostLoginURL string `envconfig:"POST_LOGIN_URL" default:"http://localhost:3000"`
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
		"host=%s port=%d user=%s password=%s dbname=%s sslmode=%s",
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
