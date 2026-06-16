-- 00004_create_user_identity.sql

-- +goose Up
CREATE TABLE user_identity (
    id          UUID PRIMARY KEY,
    user_id     UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    provider    TEXT NOT NULL,
    external_id TEXT NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (provider, external_id)
);

CREATE INDEX idx_user_identity_user_id ON user_identity (user_id);

-- +goose Down
DROP INDEX idx_user_identity_user_id;
DROP TABLE user_identity;
