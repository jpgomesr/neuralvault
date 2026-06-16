-- 00003_create_user_workspace.sql

-- +goose Up
CREATE TYPE workspace_role AS ENUM ('owner', 'admin', 'member');

CREATE TABLE user_workspace (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id         UUID NOT NULL REFERENCES users(id),
    workspace_id    UUID NOT NULL REFERENCES workspace(id),
    role            workspace_role NOT NULL DEFAULT 'member',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE user_workspace;
DROP TYPE workspace_role;