-- 00011_add_model_config.sql

-- +goose Up
-- Per-workspace provider API keys (BYOK). The key is stored encrypted with
-- AES-256-GCM (see internal/crypto); api_key_hint holds only the last few
-- characters so the UI can show which key is configured without ever
-- returning the secret.
-- A workspace holds at most one key per provider, so (workspace_id, provider)
-- is the natural primary key and upsert target — there is no surrogate id.
CREATE TABLE provider_credential (
    workspace_id       UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    provider           TEXT NOT NULL,
    api_key_ciphertext BYTEA NOT NULL,
    api_key_hint       TEXT NOT NULL,
    base_url           TEXT,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (workspace_id, provider)
);

-- Which provider/model a workspace uses. Every column is nullable and a
-- workspace with no row here falls back to the server-wide defaults from the
-- environment (Ollama + QDRANT_COLLECTION_NAME), so existing workspaces keep
-- working untouched after this migration.
--
-- embedding_dimensions and embedding_collection are stored rather than derived:
-- a Qdrant collection is created with a fixed vector size, so each embedding
-- model needs its own collection, and the dimension is discovered by probing
-- the provider when the setting is saved.
CREATE TABLE workspace_model_settings (
    workspace_id         UUID PRIMARY KEY REFERENCES workspace(id) ON DELETE CASCADE,
    llm_provider         TEXT,
    llm_model            TEXT,
    embedding_provider   TEXT,
    embedding_model      TEXT,
    embedding_dimensions INT,
    embedding_collection TEXT,
    updated_at           TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE workspace_model_settings;
DROP TABLE provider_credential;
