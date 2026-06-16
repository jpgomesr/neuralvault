-- 00001_create_users.sql

-- +goose Up
CREATE TABLE users (
   id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
   email      TEXT NOT NULL UNIQUE,
   name       TEXT NOT NULL,
   created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
   updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE users;