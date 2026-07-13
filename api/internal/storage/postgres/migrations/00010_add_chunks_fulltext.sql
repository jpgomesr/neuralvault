-- 00010_add_chunks_fulltext.sql

-- +goose Up
-- 'simple' config (lowercase + tokenize, no stemming/stop-words) is used
-- deliberately: this backs lexical/hybrid retrieval fusion, whose job is
-- catching literal technical terms (e.g. "PostgreSQL", "Qdrant") regardless
-- of the query's language, not English-specific stemming.
ALTER TABLE chunks ADD COLUMN content_tsv tsvector
    GENERATED ALWAYS AS (to_tsvector('simple', content)) STORED;

CREATE INDEX idx_chunks_content_tsv ON chunks USING GIN (content_tsv);

-- +goose Down
DROP INDEX idx_chunks_content_tsv;
ALTER TABLE chunks DROP COLUMN content_tsv;
