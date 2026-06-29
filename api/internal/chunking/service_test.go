package chunking_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jpgomesr/NeuralVault/internal/chunking"
	chunkmd "github.com/jpgomesr/NeuralVault/internal/chunking/markdown"
	"github.com/jpgomesr/NeuralVault/internal/model"
)

// integrationPool returns a live pgxpool connection, or skips the test when
// POSTGRES_HOST is not set (matches the pattern in storage/postgres/postgres_test.go).
func integrationPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	if os.Getenv("POSTGRES_HOST") == "" {
		t.Skip("POSTGRES_HOST not set; skipping chunking service integration test")
	}

	port := 5432
	if p := os.Getenv("POSTGRES_PORT"); p != "" {
		if n, err := strconv.Atoi(p); err == nil {
			port = n
		}
	}
	dsn := fmt.Sprintf(
		"host=%s port=%d user=%s password=%s dbname=%s sslmode=disable",
		getenv("POSTGRES_HOST", "localhost"),
		port,
		getenv("POSTGRES_USERNAME", "neuralvault"),
		getenv("POSTGRES_PASSWORD", "neuralvault"),
		getenv("POSTGRES_NAME", "neuralvault"),
	)
	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("creating pool: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// setupSchema creates the tables required by ChunkService in idempotent fashion.
// Enum types use a DO block because PostgreSQL has no CREATE TYPE IF NOT EXISTS.
const setupSchema = `
DO $$ BEGIN
    CREATE TYPE source_type AS ENUM ('git', 'file', 'web');
EXCEPTION WHEN duplicate_object THEN null;
END $$;

DO $$ BEGIN
    CREATE TYPE source_status AS ENUM ('pending', 'indexing', 'indexed', 'error');
EXCEPTION WHEN duplicate_object THEN null;
END $$;

CREATE TABLE IF NOT EXISTS workspace (
    id         UUID PRIMARY KEY,
    name       TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS sources (
    id           UUID PRIMARY KEY,
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    name         TEXT NOT NULL,
    type         source_type NOT NULL,
    uri          TEXT NOT NULL,
    status       source_status NOT NULL DEFAULT 'pending',
    metadata     JSONB,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS chunks (
    id              UUID PRIMARY KEY,
    source_id       UUID NOT NULL REFERENCES sources(id) ON DELETE CASCADE,
    workspace_id    UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    content         TEXT NOT NULL,
    chunk_index     INT NOT NULL,
    metadata        JSONB,
    embedding_model TEXT NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (source_id, chunk_index)
);
`

func TestChunkService(t *testing.T) {
	pool := integrationPool(t)
	ctx := context.Background()

	if _, err := pool.Exec(ctx, setupSchema); err != nil {
		t.Fatalf("applying schema: %v", err)
	}

	workspaceID := uuid.New()
	sourceID := uuid.New()

	if _, err := pool.Exec(ctx,
		`INSERT INTO workspace (id, name) VALUES ($1, $2)`,
		workspaceID, "test-workspace",
	); err != nil {
		t.Fatalf("inserting workspace: %v", err)
	}

	if _, err := pool.Exec(ctx,
		`INSERT INTO sources (id, workspace_id, name, type, uri) VALUES ($1, $2, $3, $4, $5)`,
		sourceID, workspaceID, "test-source", "file", "docs/intro.md",
	); err != nil {
		t.Fatalf("inserting source: %v", err)
	}

	t.Cleanup(func() {
		// CASCADE deletes sources and chunks automatically.
		pool.Exec(ctx, `DELETE FROM workspace WHERE id = $1`, workspaceID) //nolint:errcheck
	})

	svc := chunking.NewChunkService(pool, map[chunking.ContentType]chunking.Splitter{
		chunking.ContentTypeMarkdown: chunkmd.New(),
	})

	req := chunking.ChunkRequest{
		SourceID:    sourceID,
		WorkspaceID: workspaceID,
		Content:     "# Intro\nThis is the intro.\n\n## Setup\nInstallation steps.",
		ContentType: chunking.ContentTypeMarkdown,
		FilePath:    "docs/intro.md",
	}

	t.Run("ChunkSource", func(t *testing.T) {
		chunks, err := svc.ChunkSource(ctx, req)
		if err != nil {
			t.Fatalf("ChunkSource: %v", err)
		}
		if len(chunks) != 2 {
			t.Errorf("got %d chunks, want 2", len(chunks))
		}
		for i, ch := range chunks {
			if ch.ChunkIndex != i {
				t.Errorf("chunk[%d].ChunkIndex = %d, want %d", i, ch.ChunkIndex, i)
			}
			if ch.SourceID != sourceID {
				t.Errorf("chunk[%d].SourceID mismatch", i)
			}
		}
		// Verify metadata fields are populated (exercises buildMetadata).
		if len(chunks) > 0 {
			var meta model.FileChunkMetadata
			if err := json.Unmarshal(chunks[0].Metadata, &meta); err != nil {
				t.Fatalf("unmarshal metadata: %v", err)
			}
			if meta.FilePath != req.FilePath {
				t.Errorf("metadata.FilePath = %q, want %q", meta.FilePath, req.FilePath)
			}
			if meta.Level == 0 {
				t.Error("metadata.Level = 0, want non-zero for a headed section")
			}
			if meta.StartLine == 0 {
				t.Error("metadata.StartLine = 0, want non-zero")
			}
		}
	})

	t.Run("ChunkSource_unsupported_type", func(t *testing.T) {
		bad := req
		bad.ContentType = "pdf"
		_, err := svc.ChunkSource(ctx, bad)
		if err == nil {
			t.Error("expected error for unsupported content type, got nil")
		}
	})

	t.Run("ListChunks", func(t *testing.T) {
		chunks, err := svc.ListChunks(ctx, sourceID)
		if err != nil {
			t.Fatalf("ListChunks: %v", err)
		}
		if len(chunks) != 2 {
			t.Errorf("got %d chunks, want 2", len(chunks))
		}
		if len(chunks) > 0 && chunks[0].ChunkIndex != 0 {
			t.Errorf("first chunk ChunkIndex = %d, want 0", chunks[0].ChunkIndex)
		}
	})

	t.Run("DeleteChunks", func(t *testing.T) {
		if err := svc.DeleteChunks(ctx, sourceID); err != nil {
			t.Fatalf("DeleteChunks: %v", err)
		}
		remaining, err := svc.ListChunks(ctx, sourceID)
		if err != nil {
			t.Fatalf("ListChunks after delete: %v", err)
		}
		if len(remaining) != 0 {
			t.Errorf("expected 0 chunks after delete, got %d", len(remaining))
		}
	})
}
