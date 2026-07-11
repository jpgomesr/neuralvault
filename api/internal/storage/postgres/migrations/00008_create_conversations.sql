-- 00008_create_conversations.sql

-- +goose Up
CREATE TABLE conversations (
    id           UUID PRIMARY KEY,
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    title        TEXT NOT NULL DEFAULT '',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_conversations_workspace_id ON conversations (workspace_id);

-- +goose Down
DROP INDEX idx_conversations_workspace_id;
DROP TABLE conversations;
