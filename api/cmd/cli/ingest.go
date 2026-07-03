package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// createSourceResponse mirrors the JSON shape returned by POST /sources
// (api/internal/sources/handler.go CreateSource). model.Source has no json
// tags, so its fields serialize under their exact Go (PascalCase) names.
type createSourceResponse struct {
	Source struct {
		ID     string `json:"ID"`
		Status string `json:"Status"`
	} `json:"source"`
	StatusURL string `json:"status_url"`
}

// progressEvent mirrors sources.ProgressEvent (api/internal/sources/bus.go).
// Defined locally rather than imported so the CLI stays a pure HTTP client.
type progressEvent struct {
	Type   string `json:"type"`
	File   string `json:"file,omitempty"`
	Chunks int    `json:"chunks,omitempty"`
	Total  int    `json:"total,omitempty"`
	Error  string `json:"error,omitempty"`
}

const (
	eventIndexing = "indexing"
	eventDone     = "done"
	eventError    = "error"
)

// runIngest implements the `ingest` subcommand: uploads a file to
// POST /sources, then streams GET /sources/{id}/status until the source
// finishes indexing (or fails).
func runIngest(prog string, args []string) error {
	fs := flag.NewFlagSet("ingest", flag.ContinueOnError)
	apiURLFlag := fs.String("api-url", "", "NeuralVault API base URL (default "+defaultAPIURL+", or NEURALVAULT_API_URL)")
	workspaceIDFlag := fs.String("workspace-id", "", "workspace UUID (or NEURALVAULT_WORKSPACE_ID)")
	name := fs.String("name", "", "source name (default: file name)")
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), "usage: %s ingest [flags] <file>\n\n", prog)
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}

	if fs.NArg() != 1 {
		fs.Usage()
		return fmt.Errorf("expected exactly one file argument, got %d", fs.NArg())
	}
	filePath := fs.Arg(0)

	apiURL := resolveAPIURL(*apiURLFlag)
	workspaceID, err := resolveWorkspaceID(*workspaceIDFlag)
	if err != nil {
		return err
	}

	sourceName := *name
	if sourceName == "" {
		sourceName = filepath.Base(filePath)
	}

	req, err := buildMultipartRequest(apiURL, workspaceID.String(), sourceName, filePath)
	if err != nil {
		return err
	}

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("creating source: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusAccepted {
		return fmt.Errorf("creating source: %s (status %d)", readErrorBody(resp), resp.StatusCode)
	}

	var created createSourceResponse
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		return fmt.Errorf("decoding create-source response: %w", err)
	}

	fmt.Printf("created source %s, streaming progress...\n", created.Source.ID) //nolint:errcheck
	return streamStatus(os.Stdout, apiURL, created.StatusURL)
}

// buildMultipartRequest builds the POST /sources multipart/form-data request
// with the workspace_id, name, and files fields the server expects.
func buildMultipartRequest(apiURL, workspaceID, name, filePath string) (*http.Request, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("opening file: %w", err)
	}
	defer f.Close() //nolint:errcheck

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)

	if err := mw.WriteField("workspace_id", workspaceID); err != nil {
		return nil, fmt.Errorf("writing workspace_id field: %w", err)
	}
	if err := mw.WriteField("name", name); err != nil {
		return nil, fmt.Errorf("writing name field: %w", err)
	}
	fw, err := mw.CreateFormFile("files", filepath.Base(filePath))
	if err != nil {
		return nil, fmt.Errorf("creating file part: %w", err)
	}
	if _, err := io.Copy(fw, f); err != nil {
		return nil, fmt.Errorf("copying file content: %w", err)
	}
	if err := mw.Close(); err != nil {
		return nil, fmt.Errorf("closing multipart writer: %w", err)
	}

	req, err := http.NewRequest(http.MethodPost, apiURL+"/sources", &buf)
	if err != nil {
		return nil, fmt.Errorf("building request: %w", err)
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	return req, nil
}

// streamStatus reads the SSE stream at apiURL+statusURL, printing progress
// to w, until a terminal "done" or "error" event is received.
func streamStatus(w io.Writer, apiURL, statusURL string) error {
	// No client-side timeout: the server can hold this connection open for
	// up to 15 minutes while indexing runs, sending heartbeats every 30s.
	client := &http.Client{}
	resp, err := client.Get(apiURL + statusURL)
	if err != nil {
		return fmt.Errorf("connecting to status stream: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("status stream failed: %s (status %d)", readErrorBody(resp), resp.StatusCode)
	}

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	for scanner.Scan() {
		line := scanner.Text()
		if line == "" || strings.HasPrefix(line, ":") {
			continue // blank line or heartbeat comment
		}
		data, ok := strings.CutPrefix(line, "data: ")
		if !ok {
			continue
		}

		var event progressEvent
		if err := json.Unmarshal([]byte(data), &event); err != nil {
			return fmt.Errorf("parsing status event: %w", err)
		}

		switch event.Type {
		case eventIndexing:
			fmt.Fprintf(w, "  indexing %s (%d chunks)\n", event.File, event.Chunks) //nolint:errcheck
		case eventDone:
			fmt.Fprintf(w, "done: %d chunks total\n", event.Total) //nolint:errcheck
			return nil
		case eventError:
			return fmt.Errorf("indexing failed: %s", event.Error)
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("reading status stream: %w", err)
	}
	return fmt.Errorf("status stream closed unexpectedly before completion")
}
