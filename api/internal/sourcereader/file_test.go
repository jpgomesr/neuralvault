package sourcereader

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jpgomesr/NeuralVault/internal/chunking"
	"github.com/jpgomesr/NeuralVault/internal/model"
)

func makeSource(t *testing.T, rootPath string) model.Source {
	t.Helper()
	meta, err := json.Marshal(model.FileSourceMetadata{RootPath: rootPath})
	if err != nil {
		t.Fatalf("marshal metadata: %v", err)
	}
	return model.Source{
		ID:          uuid.New(),
		WorkspaceID: uuid.New(),
		Type:        model.SourceTypeFile,
		Metadata:    meta,
	}
}

func writeFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write file %s: %v", path, err)
	}
	return path
}

func TestFileReader_Read(t *testing.T) {
	ctx := context.Background()
	r := NewFileReader()

	tests := []struct {
		name       string
		setup      func(t *testing.T) model.Source
		wantLen    int
		wantErr    string // substring expected in error; empty means no error
		check      func(t *testing.T, source model.Source, reqs []chunking.ChunkRequest)
	}{
		{
			name: "single markdown file",
			setup: func(t *testing.T) model.Source {
				dir := t.TempDir()
				writeFile(t, dir, "notes.md", "# Hello")
				return makeSource(t, dir)
			},
			wantLen: 1,
			check: func(t *testing.T, _ model.Source, reqs []chunking.ChunkRequest) {
				if reqs[0].ContentType != chunking.ContentTypeMarkdown {
					t.Errorf("content type = %q, want %q", reqs[0].ContentType, chunking.ContentTypeMarkdown)
				}
			},
		},
		{
			name: "single plaintext file",
			setup: func(t *testing.T) model.Source {
				dir := t.TempDir()
				writeFile(t, dir, "readme.txt", "hello world")
				return makeSource(t, dir)
			},
			wantLen: 1,
			check: func(t *testing.T, _ model.Source, reqs []chunking.ChunkRequest) {
				if reqs[0].ContentType != chunking.ContentTypePlaintext {
					t.Errorf("content type = %q, want %q", reqs[0].ContentType, chunking.ContentTypePlaintext)
				}
			},
		},
		{
			name: "mixed extensions only supported ones returned",
			setup: func(t *testing.T) model.Source {
				dir := t.TempDir()
				writeFile(t, dir, "a.md", "# A")
				writeFile(t, dir, "b.txt", "B")
				writeFile(t, dir, "c.go", "package main")
				writeFile(t, dir, "d.log", "log line")
				return makeSource(t, dir)
			},
			wantLen: 2,
		},
		{
			name: "nested directories discovered recursively",
			setup: func(t *testing.T) model.Source {
				dir := t.TempDir()
				sub1 := filepath.Join(dir, "sub1")
				sub2 := filepath.Join(sub1, "sub2")
				if err := os.MkdirAll(sub2, 0o700); err != nil {
					t.Fatal(err)
				}
				writeFile(t, dir, "root.md", "root")
				writeFile(t, sub1, "level1.md", "level1")
				writeFile(t, sub2, "level2.md", "level2")
				return makeSource(t, dir)
			},
			wantLen: 3,
		},
		{
			name: "empty directory returns empty slice without error",
			setup: func(t *testing.T) model.Source {
				return makeSource(t, t.TempDir())
			},
			wantLen: 0,
		},
		{
			name: "root path does not exist",
			setup: func(t *testing.T) model.Source {
				return makeSource(t, filepath.Join(t.TempDir(), "does-not-exist"))
			},
			wantErr: "root path",
		},
		{
			name: "invalid metadata JSON",
			setup: func(t *testing.T) model.Source {
				return model.Source{
					ID:          uuid.New(),
					WorkspaceID: uuid.New(),
					Type:        model.SourceTypeFile,
					Metadata:    []byte("not-json"),
				}
			},
			wantErr: "decoding file source metadata",
		},
		{
			name: "nil metadata",
			setup: func(t *testing.T) model.Source {
				return model.Source{
					ID:          uuid.New(),
					WorkspaceID: uuid.New(),
					Type:        model.SourceTypeFile,
					Metadata:    nil,
				}
			},
			wantErr: "decoding file source metadata",
		},
		{
			name: "source IDs propagated to every request",
			setup: func(t *testing.T) model.Source {
				dir := t.TempDir()
				writeFile(t, dir, "a.md", "A")
				writeFile(t, dir, "b.md", "B")
				return makeSource(t, dir)
			},
			wantLen: 2,
			check: func(t *testing.T, source model.Source, reqs []chunking.ChunkRequest) {
				for _, req := range reqs {
					if req.SourceID != source.ID {
						t.Errorf("SourceID = %v, want %v", req.SourceID, source.ID)
					}
					if req.WorkspaceID != source.WorkspaceID {
						t.Errorf("WorkspaceID = %v, want %v", req.WorkspaceID, source.WorkspaceID)
					}
				}
			},
		},
		{
			name: "file content is read verbatim",
			setup: func(t *testing.T) model.Source {
				dir := t.TempDir()
				writeFile(t, dir, "exact.md", "# Exact Content\n\nParagraph.")
				return makeSource(t, dir)
			},
			wantLen: 1,
			check: func(t *testing.T, _ model.Source, reqs []chunking.ChunkRequest) {
				if reqs[0].Content != "# Exact Content\n\nParagraph." {
					t.Errorf("content = %q, want exact match", reqs[0].Content)
				}
			},
		},
		{
			name: "root path is a single file",
			setup: func(t *testing.T) model.Source {
				dir := t.TempDir()
				path := writeFile(t, dir, "single.md", "# Solo")
				return makeSource(t, path)
			},
			wantLen: 1,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			source := tc.setup(t)
			reqs, err := r.Read(ctx, source)

			if tc.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tc.wantErr)
				}
				if !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("error = %q, want it to contain %q", err.Error(), tc.wantErr)
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(reqs) != tc.wantLen {
				t.Errorf("got %d requests, want %d", len(reqs), tc.wantLen)
			}
			if tc.check != nil {
				tc.check(t, source, reqs)
			}
		})
	}
}

func TestContentTypeForExt(t *testing.T) {
	tests := []struct {
		ext           string
		wantType      chunking.ContentType
		wantSupported bool
	}{
		{".md", chunking.ContentTypeMarkdown, true},
		{".txt", chunking.ContentTypePlaintext, true},
		{".go", "", false},
		{".pdf", "", false},
		{"", "", false},
		{".MD", "", false}, // case-sensitive: uppercase extension not supported
	}

	for _, tc := range tests {
		t.Run(tc.ext, func(t *testing.T) {
			got, ok := contentTypeForExt(tc.ext)
			if ok != tc.wantSupported {
				t.Errorf("supported = %v, want %v", ok, tc.wantSupported)
			}
			if got != tc.wantType {
				t.Errorf("content type = %q, want %q", got, tc.wantType)
			}
		})
	}
}

func TestNewReader(t *testing.T) {
	tests := []struct {
		name       string
		sourceType model.SourceType
		wantErr    bool
	}{
		{"file source returns FileReader", model.SourceTypeFile, false},
		{"git source returns error", model.SourceTypeGit, true},
		{"web source returns error", model.SourceTypeWeb, true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			source := model.Source{Type: tc.sourceType}
			reader, err := NewReader(source)
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if reader == nil {
				t.Fatal("expected non-nil Reader")
			}
		})
	}
}
