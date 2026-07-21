package modelconfig

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/jpgomesr/neuralvault/api/internal/catalog"
	"github.com/jpgomesr/neuralvault/api/internal/crypto"
	"github.com/jpgomesr/neuralvault/api/internal/model"
	"github.com/jpgomesr/neuralvault/api/internal/storage"
)

// ErrCredentialNotFound is returned when a workspace has no key stored for the
// requested provider.
var ErrCredentialNotFound = errors.New("provider credential not found")

// store persists workspace provider credentials and model settings.
//
// It is the only place API keys cross the encryption boundary: they go in as
// plaintext and come back out as plaintext, and are ciphertext everywhere in
// between, including in the model type.
type store struct {
	pool   storage.Pool
	cipher *crypto.Cipher
}

func newStore(pool storage.Pool, cipher *crypto.Cipher) *store {
	return &store{pool: pool, cipher: cipher}
}

// hintLength is how many trailing characters of an API key are kept in
// plaintext so the UI can identify which key is stored.
const hintLength = 4

// hint returns the last hintLength characters of key, or "" if the key is too
// short to reveal any of it safely.
func hint(key string) string {
	if len(key) <= hintLength {
		return ""
	}
	return key[len(key)-hintLength:]
}

// SaveCredential encrypts apiKey and upserts it for (workspaceID, provider).
// baseURL may be empty, meaning use the catalog default.
func (s *store) SaveCredential(ctx context.Context, workspaceID uuid.UUID, provider catalog.Provider, apiKey, baseURL string) error {
	ciphertext, err := s.cipher.Encrypt([]byte(apiKey))
	if err != nil {
		return fmt.Errorf("encrypting api key: %w", err)
	}

	now := time.Now()
	_, err = s.pool.Exec(ctx, `
		INSERT INTO provider_credential
			(workspace_id, provider, api_key_ciphertext, api_key_hint, base_url, created_at, updated_at)
		VALUES ($1, $2, $3, $4, NULLIF($5, ''), $6, $6)
		ON CONFLICT (workspace_id, provider) DO UPDATE SET
			api_key_ciphertext = EXCLUDED.api_key_ciphertext,
			api_key_hint       = EXCLUDED.api_key_hint,
			base_url           = EXCLUDED.base_url,
			updated_at         = EXCLUDED.updated_at`,
		workspaceID, provider, ciphertext, hint(apiKey), baseURL, now,
	)
	if err != nil {
		// The error is deliberately not wrapped with any request detail: this
		// statement's arguments include the encrypted key.
		return fmt.Errorf("saving provider credential: %w", err)
	}
	return nil
}

// GetCredential returns the decrypted API key and base URL for
// (workspaceID, provider), or ErrCredentialNotFound.
func (s *store) GetCredential(ctx context.Context, workspaceID uuid.UUID, provider catalog.Provider) (apiKey, baseURL string, err error) {
	var ciphertext []byte
	var url *string

	err = s.pool.QueryRow(ctx, `
		SELECT api_key_ciphertext, base_url
		FROM provider_credential
		WHERE workspace_id = $1 AND provider = $2`,
		workspaceID, provider,
	).Scan(&ciphertext, &url)

	if errors.Is(err, pgx.ErrNoRows) {
		return "", "", ErrCredentialNotFound
	}
	if err != nil {
		return "", "", fmt.Errorf("querying provider credential: %w", err)
	}

	plaintext, err := s.cipher.Decrypt(ciphertext)
	if err != nil {
		// A stored key that will not decrypt means the master key changed. Say
		// so explicitly: the fix is to re-enter the key, and without this the
		// failure looks like a corrupt database.
		return "", "", fmt.Errorf("decrypting api key for provider %q (was SECRETS_ENCRYPTION_KEY rotated?): %w", provider, err)
	}

	if url != nil {
		baseURL = *url
	}
	return string(plaintext), baseURL, nil
}

// ListCredentials returns the stored credentials for a workspace, without their
// keys — only the hint. It backs the settings UI, which must never receive a key.
func (s *store) ListCredentials(ctx context.Context, workspaceID uuid.UUID) ([]model.ProviderCredential, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT workspace_id, provider, api_key_hint, COALESCE(base_url, ''), created_at, updated_at
		FROM provider_credential
		WHERE workspace_id = $1
		ORDER BY provider`,
		workspaceID,
	)
	if err != nil {
		return nil, fmt.Errorf("querying provider credentials: %w", err)
	}
	defer rows.Close()

	var out []model.ProviderCredential
	for rows.Next() {
		var c model.ProviderCredential
		if err := rows.Scan(
			&c.WorkspaceID, &c.Provider, &c.APIKeyHint,
			&c.BaseURL, &c.CreatedAt, &c.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scanning provider credential row: %w", err)
		}
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating provider credential rows: %w", err)
	}
	return out, nil
}

// DeleteCredential removes a workspace's key for one provider. Deleting a
// credential that does not exist is not an error.
func (s *store) DeleteCredential(ctx context.Context, workspaceID uuid.UUID, provider catalog.Provider) error {
	_, err := s.pool.Exec(ctx, `
		DELETE FROM provider_credential
		WHERE workspace_id = $1 AND provider = $2`,
		workspaceID, provider,
	)
	if err != nil {
		return fmt.Errorf("deleting provider credential: %w", err)
	}
	return nil
}

// GetSettings returns a workspace's model settings. A workspace with no row
// gets a zero-valued settings struct, not an error: that is the normal state
// meaning "use the server defaults".
func (s *store) GetSettings(ctx context.Context, workspaceID uuid.UUID) (model.WorkspaceModelSettings, error) {
	settings := model.WorkspaceModelSettings{WorkspaceID: workspaceID}

	var llmProvider, llmModel, embProvider, embModel, embCollection *string
	var embDimensions *int

	err := s.pool.QueryRow(ctx, `
		SELECT llm_provider, llm_model,
		       embedding_provider, embedding_model, embedding_dimensions, embedding_collection,
		       updated_at
		FROM workspace_model_settings
		WHERE workspace_id = $1`,
		workspaceID,
	).Scan(
		&llmProvider, &llmModel,
		&embProvider, &embModel, &embDimensions, &embCollection,
		&settings.UpdatedAt,
	)

	if errors.Is(err, pgx.ErrNoRows) {
		return settings, nil
	}
	if err != nil {
		return settings, fmt.Errorf("querying workspace model settings: %w", err)
	}

	if llmProvider != nil {
		settings.LLMProvider = catalog.Provider(*llmProvider)
	}
	if llmModel != nil {
		settings.LLMModel = *llmModel
	}
	if embProvider != nil {
		settings.EmbeddingProvider = catalog.Provider(*embProvider)
	}
	if embModel != nil {
		settings.EmbeddingModel = *embModel
	}
	if embDimensions != nil {
		settings.EmbeddingDimensions = uint64(*embDimensions)
	}
	if embCollection != nil {
		settings.EmbeddingCollection = *embCollection
	}

	return settings, nil
}

// SaveLLMSettings upserts only the completion half of a workspace's settings,
// leaving the embedding half untouched — the two are chosen independently, and
// changing the model has no consequences beyond the next query, while changing
// the embedding provider requires a re-index.
func (s *store) SaveLLMSettings(ctx context.Context, workspaceID uuid.UUID, provider catalog.Provider, llmModel string) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO workspace_model_settings (workspace_id, llm_provider, llm_model, updated_at)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (workspace_id) DO UPDATE SET
			llm_provider = EXCLUDED.llm_provider,
			llm_model    = EXCLUDED.llm_model,
			updated_at   = EXCLUDED.updated_at`,
		workspaceID, provider, llmModel, time.Now(),
	)
	if err != nil {
		return fmt.Errorf("saving llm settings: %w", err)
	}
	return nil
}

// SaveEmbeddingSettings upserts the embedding half of a workspace's settings.
// The collection and dimensions are written together with the model, since a
// collection is bound to exactly one model's vector size.
func (s *store) SaveEmbeddingSettings(ctx context.Context, workspaceID uuid.UUID, provider catalog.Provider, embeddingModel, collection string, dimensions uint64) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO workspace_model_settings
			(workspace_id, embedding_provider, embedding_model, embedding_dimensions, embedding_collection, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (workspace_id) DO UPDATE SET
			embedding_provider   = EXCLUDED.embedding_provider,
			embedding_model      = EXCLUDED.embedding_model,
			embedding_dimensions = EXCLUDED.embedding_dimensions,
			embedding_collection = EXCLUDED.embedding_collection,
			updated_at           = EXCLUDED.updated_at`,
		workspaceID, provider, embeddingModel, int(dimensions), collection, time.Now(),
	)
	if err != nil {
		return fmt.Errorf("saving embedding settings: %w", err)
	}
	return nil
}
