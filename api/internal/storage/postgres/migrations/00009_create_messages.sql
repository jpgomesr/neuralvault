-- 00009_create_messages.sql

-- +goose Up
CREATE TYPE message_role AS ENUM ('user', 'assistant');

CREATE TABLE messages (
    id              UUID PRIMARY KEY,
    conversation_id UUID NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
    role            message_role NOT NULL,
    content         TEXT NOT NULL,
    sources         JSONB,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_messages_conversation_id ON messages (conversation_id);

-- +goose Down
DROP INDEX idx_messages_conversation_id;
DROP TABLE messages;
DROP TYPE message_role;
