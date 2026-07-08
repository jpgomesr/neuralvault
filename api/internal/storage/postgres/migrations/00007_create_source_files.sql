-- 00007_create_source_files.sql

-- +goose Up
CREATE TABLE source_files (
    id            UUID PRIMARY KEY,
    source_id     UUID NOT NULL REFERENCES sources(id) ON DELETE CASCADE,
    workspace_id  UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    name          TEXT NOT NULL,
    size          BIGINT NOT NULL,
    content_type  TEXT NOT NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (source_id, name)
);

CREATE INDEX idx_source_files_source_id ON source_files (source_id);

-- +goose Down
DROP INDEX idx_source_files_source_id;
DROP TABLE source_files;
