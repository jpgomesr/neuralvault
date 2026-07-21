package sourcereader

import (
	"context"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/jpgomesr/neuralvault/api/internal/chunking"
	"github.com/jpgomesr/neuralvault/api/internal/model"
)

// FileReader reads content from a local directory tree.
type FileReader struct{}

// NewFileReader returns a FileReader.
func NewFileReader() *FileReader {
	return &FileReader{}
}

// Read implements Reader. It validates rootPath and walks the directory tree
// producing one ChunkRequest per supported file (.md, .txt).
func (r *FileReader) Read(ctx context.Context, source model.Source, rootPath string) ([]chunking.ChunkRequest, error) {
	if _, err := os.Stat(rootPath); err != nil {
		return nil, fmt.Errorf("root path %q: %w", rootPath, err)
	}

	var requests []chunking.ChunkRequest

	err := filepath.WalkDir(rootPath, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return fmt.Errorf("accessing %q: %w", path, err)
		}

		if d.IsDir() {
			return nil
		}

		if !d.Type().IsRegular() {
			slog.DebugContext(ctx, "skipping non-regular file", "path", path)
			return nil
		}

		contentType, ok := contentTypeForExt(filepath.Ext(path))
		if !ok {
			slog.DebugContext(ctx, "skipping unsupported extension",
				"path", path,
				"ext", filepath.Ext(path),
			)
			return nil
		}

		content, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("reading %q: %w", path, err)
		}

		// Store the path relative to rootPath so chunk metadata carries the
		// same stable identifier as source_files.name, rather than the
		// ephemeral absolute temp-dir path (which changes every re-ingest).
		rel, err := filepath.Rel(rootPath, path)
		if err != nil || rel == "." {
			// rootPath is the file itself (single-file root) or an unexpected
			// mismatch: fall back to the base name.
			rel = filepath.Base(path)
		}
		rel = filepath.ToSlash(rel) // object keys / metadata always use "/"

		requests = append(requests, chunking.ChunkRequest{
			SourceID:    source.ID,
			WorkspaceID: source.WorkspaceID,
			Content:     string(content),
			ContentType: contentType,
			FilePath:    rel,
		})
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("reading file source: %w", err)
	}

	return requests, nil
}

// contentTypeForExt maps a file extension (including the dot) to a ContentType.
// Returns false if the extension is not supported.
func contentTypeForExt(ext string) (chunking.ContentType, bool) {
	switch ext {
	case ".md":
		return chunking.ContentTypeMarkdown, true
	case ".txt":
		return chunking.ContentTypePlaintext, true
	default:
		return "", false
	}
}
