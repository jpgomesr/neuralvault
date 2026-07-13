// Package sourcereader reads raw content from a registered model.Source
// and returns a list of chunking.ChunkRequest values ready to be passed to
// ChunkService.ChunkSource.
package sourcereader

import (
	"context"
	"fmt"

	"github.com/jpgomesr/NeuralVault/internal/chunking"
	"github.com/jpgomesr/NeuralVault/internal/model"
)

// Reader converts a registered model.Source into chunk requests.
// Implementations must be safe for concurrent use.
//
// rootPath is the local directory tree to read from. It is runtime pipeline
// state (an ephemeral temp dir), passed explicitly rather than persisted on the
// source; file-based readers walk it, other reader types may ignore it.
type Reader interface {
	Read(ctx context.Context, source model.Source, rootPath string) ([]chunking.ChunkRequest, error)
}

// NewReader returns the Reader appropriate for source.Type.
// Returns an error for source types that are not yet implemented (git, web).
func NewReader(source model.Source) (Reader, error) {
	switch source.Type {
	case model.SourceTypeFile:
		return NewFileReader(), nil
	default:
		return nil, fmt.Errorf("unsupported source type: %q", source.Type)
	}
}
