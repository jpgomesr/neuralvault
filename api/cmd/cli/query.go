package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"strings"
	"time"
)

const defaultTopK = 5

// queryRequestBody mirrors retrieval.queryRequest (api/internal/retrieval/handler.go).
type queryRequestBody struct {
	WorkspaceID string `json:"workspace_id"`
	Question    string `json:"question"`
	TopK        int    `json:"top_k"`
}

// queryResultItem mirrors retrieval.queryResultItem (api/internal/retrieval/handler.go).
type queryResultItem struct {
	ChunkID string  `json:"chunk_id"`
	Content string  `json:"content"`
	Score   float32 `json:"score"`
}

// queryResponseBody mirrors retrieval.queryResponse (api/internal/retrieval/handler.go).
type queryResponseBody struct {
	Results []queryResultItem `json:"results"`
}

// runQuery implements the `query` subcommand: sends a semantic search
// question to POST /query and prints the ranked results.
func runQuery(prog string, args []string) error {
	fs := flag.NewFlagSet("query", flag.ContinueOnError)
	apiURLFlag := fs.String("api-url", "", "NeuralVault API base URL (default "+defaultAPIURL+", or NEURALVAULT_API_URL)")
	workspaceIDFlag := fs.String("workspace-id", "", "workspace UUID (or NEURALVAULT_WORKSPACE_ID)")
	topK := fs.Int("top-k", defaultTopK, "number of results to return")
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), "usage: %s query [flags] \"<question>\"\n\n", prog)
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}

	if fs.NArg() != 1 {
		fs.Usage()
		return fmt.Errorf("expected exactly one question argument, got %d", fs.NArg())
	}
	question := fs.Arg(0)

	apiURL := resolveAPIURL(*apiURLFlag)
	workspaceID, err := resolveWorkspaceID(*workspaceIDFlag)
	if err != nil {
		return err
	}

	reqBody, err := json.Marshal(queryRequestBody{
		WorkspaceID: workspaceID.String(),
		Question:    question,
		TopK:        *topK,
	})
	if err != nil {
		return fmt.Errorf("encoding query request: %w", err)
	}

	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := postJSON(client, apiURL, "/query", reqBody)
	if err != nil {
		return fmt.Errorf("running query: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("query failed: %s (status %d)", readErrorBody(resp), resp.StatusCode)
	}

	var result queryResponseBody
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("decoding query response: %w", err)
	}

	fmt.Print(formatResults(question, result.Results)) //nolint:errcheck
	return nil
}

// formatResults renders query results in the CLI's human-readable output
// format: a header with the question, then each result numbered with its
// similarity score and content.
func formatResults(question string, results []queryResultItem) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Query:\n%s\n\nResults:\n\n", question)
	if len(results) == 0 {
		b.WriteString("No results found.\n")
		return b.String()
	}
	for i, r := range results {
		fmt.Fprintf(&b, "[%d] Score: %.2f\n%s\n\n", i+1, r.Score, r.Content)
	}
	return b.String()
}
