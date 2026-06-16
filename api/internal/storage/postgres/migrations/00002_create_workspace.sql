-- 00002_create_workspace.sql

-- +goose Up
CREATE TABLE workspace (
   id         UUID PRIMARY KEY,
   name       TEXT NOT NULL,
   created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
   updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE workspace;