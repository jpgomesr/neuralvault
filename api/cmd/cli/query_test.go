package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestFormatResults(t *testing.T) {
	tests := []struct {
		name     string
		question string
		results  []queryResultItem
		want     string
	}{
		{
			name:     "no results",
			question: "how does postgresql work?",
			results:  nil,
			want:     "Query:\nhow does postgresql work?\n\nResults:\n\nNo results found.\n",
		},
		{
			name:     "one result",
			question: "how does replication work?",
			results: []queryResultItem{
				{ChunkID: "c1", Content: "PostgreSQL supports replication through...", Score: 0.92},
			},
			want: "Query:\nhow does replication work?\n\nResults:\n\n" +
				"[1] Score: 0.92\nPostgreSQL supports replication through...\n\n",
		},
		{
			name:     "multiple results",
			question: "how does replication work?",
			results: []queryResultItem{
				{ChunkID: "c1", Content: "PostgreSQL supports replication through...", Score: 0.92},
				{ChunkID: "c2", Content: "Streaming replication allows...", Score: 0.87},
			},
			want: "Query:\nhow does replication work?\n\nResults:\n\n" +
				"[1] Score: 0.92\nPostgreSQL supports replication through...\n\n" +
				"[2] Score: 0.87\nStreaming replication allows...\n\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatResults(tt.question, tt.results)
			if got != tt.want {
				t.Fatalf("formatResults() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestRunQuery(t *testing.T) {
	workspaceID := uuid.New()

	t.Run("success", func(t *testing.T) {
		var gotBody queryRequestBody
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodPost || r.URL.Path != "/query" {
				t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
			}
			if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
				t.Fatalf("decoding request body: %v", err)
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(queryResponseBody{ //nolint:errcheck
				Results: []queryResultItem{
					{ChunkID: "c1", Content: "PostgreSQL supports replication through...", Score: 0.92},
				},
			})
		}))
		defer server.Close()

		err := runQuery("neuralvault", []string{
			"--api-url", server.URL,
			"--workspace-id", workspaceID.String(),
			"--top-k", "3",
			"How does PostgreSQL work?",
		})
		if err != nil {
			t.Fatalf("runQuery() error = %v", err)
		}
		if gotBody.WorkspaceID != workspaceID.String() {
			t.Errorf("workspace_id = %q, want %q", gotBody.WorkspaceID, workspaceID.String())
		}
		if gotBody.Question != "How does PostgreSQL work?" {
			t.Errorf("question = %q", gotBody.Question)
		}
		if gotBody.TopK != 3 {
			t.Errorf("top_k = %d, want 3", gotBody.TopK)
		}
	})

	t.Run("non-2xx returns error with body", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "question is required", http.StatusBadRequest)
		}))
		defer server.Close()

		err := runQuery("neuralvault", []string{
			"--api-url", server.URL,
			"--workspace-id", workspaceID.String(),
			"anything",
		})
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !strings.Contains(err.Error(), "question is required") {
			t.Errorf("error = %v, want it to contain server message", err)
		}
	})

	t.Run("missing workspace id errors before request", func(t *testing.T) {
		err := runQuery("neuralvault", []string{"anything"})
		if err == nil {
			t.Fatal("expected error, got nil")
		}
	})

	t.Run("requires exactly one positional arg", func(t *testing.T) {
		err := runQuery("neuralvault", []string{
			"--workspace-id", workspaceID.String(),
		})
		if err == nil {
			t.Fatal("expected error for missing question, got nil")
		}
	})
}
