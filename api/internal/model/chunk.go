package model

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// GitChunkMetadata identifies the exact location of a chunk within a git source.
type GitChunkMetadata struct {
	FilePath  string `json:"file_path"`
	StartLine int    `json:"start_line"`
	EndLine   int    `json:"end_line"`
	CommitHash string `json:"commit_hash"`
}

// FileChunkMetadata identifies the location of a chunk within a file source.
type FileChunkMetadata struct {
	FilePath  string `json:"file_path"`
	Page      int    `json:"page,omitempty"`
	Heading   string `json:"heading,omitempty"`
	Level     int    `json:"level,omitempty"`      // ATX heading level 1-6; 0 means no heading
	StartLine int    `json:"start_line,omitempty"` // 1-based; 0 means not tracked
	EndLine   int    `json:"end_line,omitempty"`   // 1-based inclusive; 0 means not tracked
}

// WebChunkMetadata identifies the location of a chunk within a web source.
type WebChunkMetadata struct {
	URL   string `json:"url"`
	Title string `json:"title,omitempty"`
}

type Chunk struct {
	// ID is also used as the Qdrant point ID, establishing a 1:1 mapping.
	ID             uuid.UUID       `db:"id"`
	SourceID       uuid.UUID       `db:"source_id"`
	WorkspaceID    uuid.UUID       `db:"workspace_id"`
	Content        string          `db:"content"`
	ChunkIndex     int             `db:"chunk_index"`
	Metadata       json.RawMessage `db:"metadata"`
	EmbeddingModel string          `db:"embedding_model"`
	CreatedAt      time.Time       `db:"created_at"`
}
