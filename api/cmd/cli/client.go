package main

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/google/uuid"
)

const defaultAPIURL = "http://localhost:8080"

// resolveAPIURL returns the API base URL to target: the flag value if set,
// otherwise NEURALVAULT_API_URL, otherwise defaultAPIURL.
func resolveAPIURL(flagVal string) string {
	if flagVal != "" {
		return strings.TrimSuffix(flagVal, "/")
	}
	if env := os.Getenv("NEURALVAULT_API_URL"); env != "" {
		return strings.TrimSuffix(env, "/")
	}
	return defaultAPIURL
}

// resolveWorkspaceID returns the workspace UUID to use: the flag value if
// set, otherwise NEURALVAULT_WORKSPACE_ID. Returns an error if neither is
// set or the value isn't a valid UUID.
func resolveWorkspaceID(flagVal string) (uuid.UUID, error) {
	val := flagVal
	if val == "" {
		val = os.Getenv("NEURALVAULT_WORKSPACE_ID")
	}
	if val == "" {
		return uuid.Nil, fmt.Errorf("workspace id required: set --workspace-id or NEURALVAULT_WORKSPACE_ID")
	}
	id, err := uuid.Parse(val)
	if err != nil {
		return uuid.Nil, fmt.Errorf("invalid workspace id %q: must be a UUID", val)
	}
	return id, nil
}

// readErrorBody reads a non-2xx response body as plain text, matching this
// API's convention of returning http.Error strings (not JSON) on failure.
func readErrorBody(resp *http.Response) string {
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if err != nil {
		return fmt.Sprintf("<failed to read response body: %v>", err)
	}
	return strings.TrimSpace(string(body))
}

// postJSON POSTs a JSON body to apiURL+path and returns the raw response.
// Callers are responsible for checking resp.StatusCode and closing resp.Body.
func postJSON(client *http.Client, apiURL, path string, body []byte) (*http.Response, error) {
	req, err := http.NewRequest(http.MethodPost, apiURL+path, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("building request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("sending request: %w", err)
	}
	return resp, nil
}
