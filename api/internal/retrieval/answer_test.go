package retrieval

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
	qdrantpb "github.com/qdrant/go-client/qdrant"

	"github.com/jpgomesr/NeuralVault/internal/llm"
)

// fakeProvider is an llm.Provider that returns a preset stream, recording the
// request it was handed so tests can assert the prompt that was built.
type fakeProvider struct {
	stream <-chan llm.StreamChunk
	err    error
	gotReq llm.CompletionRequest
}

func (f *fakeProvider) Complete(context.Context, llm.CompletionRequest) (llm.CompletionResponse, error) {
	return llm.CompletionResponse{}, nil
}

func (f *fakeProvider) Stream(_ context.Context, req llm.CompletionRequest) (<-chan llm.StreamChunk, error) {
	f.gotReq = req
	return f.stream, f.err
}

// emptyQuery is a vector-store query that returns no matches, so Answer's
// retrieval step short-circuits before touching Postgres.
func emptyQuery(context.Context, *qdrantpb.QueryPoints) ([]*qdrantpb.ScoredPoint, error) {
	return nil, nil
}

func TestAnswer_StreamsCompletion(t *testing.T) {
	ctx := context.Background()

	ch := make(chan llm.StreamChunk, 1)
	ch <- llm.StreamChunk{Done: true}
	close(ch)
	prov := &fakeProvider{stream: ch}

	vs := fakeVectorStore{queryFn: emptyQuery}
	svc := newTestService(sharedPool, fixedEmbedder{vector: vec(1.0)}, vs, prov, passthroughReranker, sharedQdrantCfg.CollectionName, "answer-model")

	chunks, stream, err := svc.Answer(ctx, RetrieveRequest{WorkspaceID: uuid.New(), Query: "hi"})
	if err != nil {
		t.Fatalf("Answer: %v", err)
	}
	if len(chunks) != 0 {
		t.Fatalf("expected no grounding chunks, got %d", len(chunks))
	}
	if got := <-stream; !got.Done {
		t.Fatalf("expected the terminal Done chunk, got %+v", got)
	}
	if prov.gotReq.Model != "answer-model" {
		t.Errorf("completion model: got %q, want %q", prov.gotReq.Model, "answer-model")
	}
	if len(prov.gotReq.Messages) != 2 {
		t.Fatalf("expected system+user messages, got %d", len(prov.gotReq.Messages))
	}
}

func TestAnswer_RetrieveError(t *testing.T) {
	svc := newTestService(sharedPool, failingEmbedder{}, sharedVecStore, &fakeProvider{}, passthroughReranker, sharedQdrantCfg.CollectionName, "m")

	_, _, err := svc.Answer(context.Background(), RetrieveRequest{WorkspaceID: uuid.New(), Query: "hi"})
	if err == nil || !strings.Contains(err.Error(), "retrieving context") {
		t.Fatalf("Answer: got %v, want a retrieving context error", err)
	}
}

func TestAnswer_StreamStartError(t *testing.T) {
	prov := &fakeProvider{err: errTest("provider unavailable")}
	vs := fakeVectorStore{queryFn: emptyQuery}
	svc := newTestService(sharedPool, fixedEmbedder{vector: vec(1.0)}, vs, prov, passthroughReranker, sharedQdrantCfg.CollectionName, "m")

	_, stream, err := svc.Answer(context.Background(), RetrieveRequest{WorkspaceID: uuid.New(), Query: "hi"})
	if err == nil || !strings.Contains(err.Error(), "starting completion stream") {
		t.Fatalf("Answer: got %v, want a starting completion stream error", err)
	}
	if stream != nil {
		t.Error("expected a nil stream when the completion fails to start")
	}
}
