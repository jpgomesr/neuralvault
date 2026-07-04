package main

import (
	"bytes"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeTempFile(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("writing temp file: %v", err)
	}
	return path
}

func TestBuildMultipartRequest(t *testing.T) {
	t.Run("valid file", func(t *testing.T) {
		path := writeTempFile(t, "README.md", "# hello world")

		req, err := buildMultipartRequest("http://api.example", "ws-1", "README.md", path)
		if err != nil {
			t.Fatalf("buildMultipartRequest() error = %v", err)
		}
		if req.URL.String() != "http://api.example/sources" {
			t.Errorf("URL = %q", req.URL.String())
		}

		_, params, err := mime.ParseMediaType(req.Header.Get("Content-Type"))
		if err != nil {
			t.Fatalf("parsing content type: %v", err)
		}
		mr := multipart.NewReader(req.Body, params["boundary"])
		form, err := mr.ReadForm(1 << 20)
		if err != nil {
			t.Fatalf("reading multipart form: %v", err)
		}

		if got := form.Value["workspace_id"]; len(got) != 1 || got[0] != "ws-1" {
			t.Errorf("workspace_id = %v", got)
		}
		if got := form.Value["name"]; len(got) != 1 || got[0] != "README.md" {
			t.Errorf("name = %v", got)
		}
		files := form.File["files"]
		if len(files) != 1 {
			t.Fatalf("expected 1 file part, got %d", len(files))
		}
		f, err := files[0].Open()
		if err != nil {
			t.Fatalf("opening file part: %v", err)
		}
		content, err := io.ReadAll(f)
		if err != nil {
			t.Fatalf("reading file part: %v", err)
		}
		if string(content) != "# hello world" {
			t.Errorf("file content = %q", content)
		}
	})

	t.Run("missing file errors", func(t *testing.T) {
		_, err := buildMultipartRequest("http://api.example", "ws-1", "name", filepath.Join(t.TempDir(), "does-not-exist.md"))
		if err == nil {
			t.Fatal("expected error, got nil")
		}
	})
}

func TestStreamStatus(t *testing.T) {
	t.Run("indexing then done", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			_, _ = fmt.Fprint(w, "data: {\"type\":\"indexing\",\"file\":\"README.md\",\"chunks\":3}\n\n")
			_, _ = fmt.Fprint(w, ": heartbeat\n\n")
			_, _ = fmt.Fprint(w, "data: {\"type\":\"done\",\"total\":3}\n\n")
		}))
		defer server.Close()

		var buf bytes.Buffer
		err := streamStatus(&buf, server.URL, "/sources/x/status")
		if err != nil {
			t.Fatalf("streamStatus() error = %v", err)
		}
		out := buf.String()
		if !strings.Contains(out, "indexing README.md (3 chunks)") {
			t.Errorf("output missing indexing line: %q", out)
		}
		if !strings.Contains(out, "done: 3 chunks total") {
			t.Errorf("output missing done line: %q", out)
		}
	})

	t.Run("error event", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			_, _ = fmt.Fprint(w, "data: {\"type\":\"error\",\"error\":\"indexing failed\"}\n\n")
		}))
		defer server.Close()

		var buf bytes.Buffer
		err := streamStatus(&buf, server.URL, "/sources/x/status")
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !strings.Contains(err.Error(), "indexing failed") {
			t.Errorf("error = %v", err)
		}
	})

	t.Run("closed without terminal event", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			_, _ = fmt.Fprint(w, "data: {\"type\":\"indexing\",\"file\":\"a.md\",\"chunks\":1}\n\n")
		}))
		defer server.Close()

		var buf bytes.Buffer
		err := streamStatus(&buf, server.URL, "/sources/x/status")
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !strings.Contains(err.Error(), "closed unexpectedly") {
			t.Errorf("error = %v", err)
		}
	})
}
