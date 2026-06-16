-- 00003_create_user_workspace.sql

-- +goose Up
CREATE TYPE workspace_role AS ENUM ('owner', 'admin', 'member');

CREATE TABLE user_workspace (
    id              UUID PRIMARY KEY,
    user_id         UUID NOT NULL REFERENCES users(id),
    workspace_id    UUID NOT NULL REFERENCES workspace(id),
    role            workspace_role NOT NULL DEFAULT 'member',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (user_id, workspace_id)
);

CREATE INDEX idx_user_workspace_workspace_id ON user_workspace (workspace_id);

-- +goose Down
DROP INDEX idx_user_workspace_workspace_id;
DROP TABLE user_workspace;
DROP TYPE workspace_role;