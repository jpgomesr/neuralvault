-- 00006_create_chunks.sql

-- +goose Up
CREATE TABLE chunks (
    -- id is also used as the Qdrant point ID, establishing a 1:1 mapping
    id              UUID PRIMARY KEY,
    source_id       UUID NOT NULL REFERENCES sources(id) ON DELETE CASCADE,
    workspace_id    UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    content         TEXT NOT NULL,
    chunk_index     INT NOT NULL,
    metadata        JSONB,
    embedding_model TEXT NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (source_id, chunk_index)
);

CREATE INDEX idx_chunks_source_id ON chunks (source_id);
CREATE INDEX idx_chunks_workspace_id ON chunks (workspace_id);

-- +goose Down
DROP INDEX idx_chunks_workspace_id;
DROP INDEX idx_chunks_source_id;
DROP TABLE chunks;
