package model

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

type SourceType string

const (
	SourceTypeGit  SourceType = "git"
	SourceTypeFile SourceType = "file"
	SourceTypeWeb  SourceType = "web"
)

type SourceStatus string

const (
	SourceStatusPending  SourceStatus = "pending"
	SourceStatusIndexing SourceStatus = "indexing"
	SourceStatusIndexed  SourceStatus = "indexed"
	SourceStatusError    SourceStatus = "error"
)

// GitSourceMetadata holds provider-specific config for git sources.
type GitSourceMetadata struct {
	Branch string `json:"branch"`
}

// FileSourceMetadata holds provider-specific config for file sources (PDFs, Obsidian vaults, directories).
type FileSourceMetadata struct {
	RootPath string `json:"root_path"`
}

// WebSourceMetadata holds provider-specific config for web sources.
type WebSourceMetadata struct {
	CrawlDepth   int      `json:"crawl_depth"`
	AllowedPaths []string `json:"allowed_paths,omitempty"`
}

type Source struct {
	ID          uuid.UUID        `db:"id"`
	WorkspaceID uuid.UUID        `db:"workspace_id"`
	Name        string           `db:"name"`
	Type        SourceType       `db:"type"`
	URI         string           `db:"uri"`
	Status      SourceStatus     `db:"status"`
	Metadata    json.RawMessage  `db:"metadata"`
	CreatedAt   time.Time        `db:"created_at"`
	UpdatedAt   time.Time        `db:"updated_at"`
}
