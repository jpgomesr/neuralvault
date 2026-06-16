-- 00005_create_sources.sql

-- +goose Up
CREATE TYPE source_type AS ENUM ('git', 'file', 'web');
CREATE TYPE source_status AS ENUM ('pending', 'indexing', 'indexed', 'error');

CREATE TABLE sources (
    id              UUID PRIMARY KEY,
    workspace_id    UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    name            TEXT NOT NULL,
    type            source_type NOT NULL,
    uri             TEXT NOT NULL,
    status          source_status NOT NULL DEFAULT 'pending',
    metadata        JSONB,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_sources_workspace_id ON sources (workspace_id);
CREATE INDEX idx_sources_status ON sources (status);

-- +goose Down
DROP INDEX idx_sources_status;
DROP INDEX idx_sources_workspace_id;
DROP TABLE sources;
DROP TYPE source_status;
DROP TYPE source_type;
