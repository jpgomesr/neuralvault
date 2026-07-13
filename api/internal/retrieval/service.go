// Package retrieval implements the search engine: it embeds a query, runs a
// hybrid (vector + lexical) workspace-scoped search, fuses and reranks the
// candidates, and hydrates the results with chunk content from Postgres.
package retrieval

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	qdrantpb "github.com/qdrant/go-client/qdrant"

	"github.com/jpgomesr/NeuralVault/internal/embedding"
	"github.com/jpgomesr/NeuralVault/internal/llm"
	"github.com/jpgomesr/NeuralVault/internal/model"
	"github.com/jpgomesr/NeuralVault/internal/reranking"
	"github.com/jpgomesr/NeuralVault/internal/storage"
	"github.com/jpgomesr/NeuralVault/internal/vectorstorage"
)

const (
	defaultTopK = 5
	maxTopK     = 50

	// candidatePoolMultiplier and minCandidatePool size the vector and lexical
	// candidate pools fed into fusion — wider than the final topK, so
	// Reciprocal Rank Fusion (see Retrieve) has room to pull in a chunk that
	// scores well lexically but falls outside the vector top-K, or vice versa.
	candidatePoolMultiplier = 4
	minCandidatePool        = 20

	// rrfK is the Reciprocal Rank Fusion damping constant — the commonly used
	// default from IR literature (Cormack et al.), included here as-is rather
	// than tuned, since it's a well-established starting point.
	rrfK = 60
)

// candidatePoolSize returns how many candidates to pull from each ranking
// signal (vector, lexical) before fusing and truncating to topK.
func candidatePoolSize(topK int) int {
	pool := topK * candidatePoolMultiplier
	if pool < minCandidatePool {
		pool = minCandidatePool
	}
	return pool
}

// RetrieveRequest carries the parameters for a semantic search over a workspace's chunks.
type RetrieveRequest struct {
	WorkspaceID uuid.UUID
	Query       string
	// TopK is the number of results to return. Values <= 0 fall back to
	// defaultTopK; values above maxTopK are capped.
	TopK int
}

// RetrievedChunk pairs a chunk with its relevance score: the cross-encoder
// reranker's score when reranking succeeded, otherwise the vector cosine
// similarity (0 for a chunk that was a lexical-only match). Both are
// normalized to roughly a 0-1 scale, so the field's meaning as a display
// "confidence" number stays consistent to callers either way.
type RetrievedChunk struct {
	Chunk model.Chunk
	Score float32
}

// Retriever embeds a query, searches Qdrant scoped to a workspace, and hydrates
// the results with chunk content from Postgres. Answer additionally streams a
// grounded LLM completion built from the retrieved context.
type Retriever interface {
	Retrieve(ctx context.Context, req RetrieveRequest) ([]RetrievedChunk, error)
	Answer(ctx context.Context, req RetrieveRequest) ([]RetrievedChunk, <-chan llm.StreamChunk, error)
}

// RetrievalService is the concrete implementation of Retriever.
type RetrievalService struct {
	pool            storage.Pool
	embedder        embedding.Embedder
	vectorStore     vectorstorage.Client
	provider        llm.Provider
	reranker        reranking.Reranker
	collectionName  string
	completionModel string
}

// NewRetrievalService constructs a RetrievalService. provider and
// completionModel back the streaming Answer flow; Retrieve does not use them.
func NewRetrievalService(pool storage.Pool, embedder embedding.Embedder, vectorStore vectorstorage.Client, provider llm.Provider, reranker reranking.Reranker, collectionName, completionModel string) *RetrievalService {
	return &RetrievalService{
		pool:            pool,
		embedder:        embedder,
		vectorStore:     vectorStore,
		provider:        provider,
		reranker:        reranker,
		collectionName:  collectionName,
		completionModel: completionModel,
	}
}

// Retrieve embeds req.Query, runs a workspace-scoped semantic search in
// Qdrant plus a lexical full-text search in Postgres, fuses the two ranked
// candidate lists via Reciprocal Rank Fusion, reranks the fused set with a
// cross-encoder, and returns the top-k chunks. The lexical signal exists
// because pure vector similarity systematically under-ranks structurally
// "dense" content (tables, diagrams) relative to short generic prose, even
// when the dense content literally contains the query's terms. Reranking
// exists because hybrid fusion still can't catch a chunk that's genuinely
// relevant but shares no literal vocabulary with the query at all — a
// cross-encoder that jointly attends to (query, chunk) pairs can make that
// semantic connection where cosine and lexical scores both come up empty.
func (s *RetrievalService) Retrieve(ctx context.Context, req RetrieveRequest) ([]RetrievedChunk, error) {
	topK := req.TopK
	if topK <= 0 {
		topK = defaultTopK
	}
	if topK > maxTopK {
		topK = maxTopK
	}
	pool := candidatePoolSize(topK)

	vector, err := s.embedder.Embed(ctx, req.Query)
	if err != nil {
		return nil, fmt.Errorf("embedding query: %w", err)
	}

	start := time.Now()
	scoredPoints, err := s.vectorStore.Query(ctx, &qdrantpb.QueryPoints{
		CollectionName: s.collectionName,
		Query:          qdrantpb.NewQuery(vector...),
		Filter: &qdrantpb.Filter{
			Must: []*qdrantpb.Condition{qdrantpb.NewMatch("workspace_id", req.WorkspaceID.String())},
		},
		Limit:       qdrantpb.PtrOf(uint64(pool)),
		WithPayload: qdrantpb.NewWithPayload(true),
	})
	if err != nil {
		slog.ErrorContext(ctx, "qdrant query failed", "err", err, "workspace_id", req.WorkspaceID)
		return nil, fmt.Errorf("qdrant query: %w", err)
	}
	slog.DebugContext(ctx, "qdrant query completed",
		"workspace_id", req.WorkspaceID,
		"pool_size", pool,
		"result_count", len(scoredPoints),
		"duration_ms", time.Since(start).Milliseconds(),
	)

	vectorIDs := make([]uuid.UUID, 0, len(scoredPoints))
	vectorScores := make(map[uuid.UUID]float32, len(scoredPoints))
	vectorRank := make(map[uuid.UUID]int, len(scoredPoints))
	for i, p := range scoredPoints {
		rawID := p.GetId().GetUuid()
		id, err := uuid.Parse(rawID)
		if err != nil {
			return nil, fmt.Errorf("parsing point id %q: %w", rawID, err)
		}
		vectorIDs = append(vectorIDs, id)
		vectorScores[id] = p.GetScore()
		vectorRank[id] = i
	}

	lexicalIDs, err := s.lexicalSearch(ctx, req.WorkspaceID, req.Query, pool)
	if err != nil {
		return nil, fmt.Errorf("lexical search: %w", err)
	}
	lexicalRank := make(map[uuid.UUID]int, len(lexicalIDs))
	for i, id := range lexicalIDs {
		lexicalRank[id] = i
	}

	if len(vectorIDs) == 0 && len(lexicalIDs) == 0 {
		slog.InfoContext(ctx, "no matches found", "workspace_id", req.WorkspaceID)
		return []RetrievedChunk{}, nil
	}

	// Union the two candidate lists (vector first) and fuse each chunk's
	// per-list rank into a single score via Reciprocal Rank Fusion. ids'
	// insertion order also serves as a deterministic tie-breaker below, since
	// Postgres row order from loadChunks is not guaranteed to match it.
	ids := make([]uuid.UUID, 0, len(vectorIDs)+len(lexicalIDs))
	idsIndex := make(map[uuid.UUID]int, len(vectorIDs)+len(lexicalIDs))
	fused := make(map[uuid.UUID]float64, len(vectorIDs)+len(lexicalIDs))
	for _, id := range vectorIDs {
		if _, ok := idsIndex[id]; !ok {
			idsIndex[id] = len(ids)
			ids = append(ids, id)
		}
		fused[id] += 1.0 / float64(rrfK+vectorRank[id]+1)
	}
	for _, id := range lexicalIDs {
		if _, ok := idsIndex[id]; !ok {
			idsIndex[id] = len(ids)
			ids = append(ids, id)
		}
		fused[id] += 1.0 / float64(rrfK+lexicalRank[id]+1)
	}

	chunks, err := s.loadChunks(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("loading chunks: %w", err)
	}

	// Filtering on WorkspaceID again is defense-in-depth: it should already be
	// guaranteed by the Qdrant filter and lexicalSearch's WHERE clause above,
	// but a chunk row is never returned to a caller outside its workspace even
	// if either filter were ever wrong.
	results := make([]RetrievedChunk, 0, len(chunks))
	for _, chunk := range chunks {
		if chunk.WorkspaceID != req.WorkspaceID {
			continue
		}
		// Score starts as the chunk's vector cosine similarity (0 if it was
		// lexical-only) — rerank overwrites it with the reranker's relevance
		// score if reranking succeeds. The internal RRF fusion score is never
		// surfaced here; it's on a different, much smaller scale and only
		// meaningful as this function's own relative ranking signal.
		results = append(results, RetrievedChunk{Chunk: chunk, Score: vectorScores[chunk.ID]})
	}
	sort.Slice(results, func(i, j int) bool {
		a, b := results[i].Chunk.ID, results[j].Chunk.ID
		if fused[a] != fused[b] {
			return fused[a] > fused[b]
		}
		return idsIndex[a] < idsIndex[b]
	})

	results = s.rerank(ctx, req.Query, results)

	if len(results) > topK {
		results = results[:topK]
	}

	return results, nil
}

// rerank scores candidates (already sorted by fused hybrid rank) against
// query with the cross-encoder reranker, re-sorts by that score, and
// overwrites each RetrievedChunk's Score with it. If the reranker errors, it
// logs a warning and returns candidates unchanged — a degraded (hybrid-only)
// ranking is preferable to failing the whole request over a reranker hiccup.
func (s *RetrievalService) rerank(ctx context.Context, query string, candidates []RetrievedChunk) []RetrievedChunk {
	if len(candidates) == 0 {
		return candidates
	}

	rerankInput := make([]reranking.Candidate, len(candidates))
	for i, c := range candidates {
		rerankInput[i] = reranking.Candidate{ID: c.Chunk.ID.String(), Text: c.Chunk.Content}
	}

	rerankResults, err := s.reranker.Rerank(ctx, query, rerankInput)
	if err != nil {
		slog.WarnContext(ctx, "reranking failed, falling back to hybrid ranking", "err", err)
		return candidates
	}

	scoreByID := make(map[string]float32, len(rerankResults))
	for _, r := range rerankResults {
		scoreByID[r.CandidateID] = r.Score
	}

	sort.SliceStable(candidates, func(i, j int) bool {
		return scoreByID[candidates[i].Chunk.ID.String()] > scoreByID[candidates[j].Chunk.ID.String()]
	})
	for i := range candidates {
		candidates[i].Score = scoreByID[candidates[i].Chunk.ID.String()]
	}

	return candidates
}

// systemPrompt frames the assistant so it answers strictly from the retrieved
// context and admits when the context is insufficient, rather than inventing.
// The numbered context blocks are for the model's internal reference only —
// sources are surfaced to the user separately by the caller, so the model
// must never echo the "[N]" markers or quote the raw blocks back.
const systemPrompt = "You are NeuralVault's assistant. Answer the user's question using ONLY the information in the numbered context blocks below. " +
	"Write a direct, natural-language answer in the same language as the question. " +
	"Do not quote or repeat the context blocks verbatim, do not reference their \"[N]\" numbers, and do not describe or analyze the context itself — just answer the question. " +
	"If the context does not contain the answer, say so concisely instead of guessing."

// Answer runs retrieval for req, then streams a grounded LLM completion built
// from the retrieved chunks. It returns the chunks (so the caller can surface
// sources immediately) alongside a channel of incremental completion chunks;
// the channel is closed once the model finishes or emits an error.
func (s *RetrievalService) Answer(ctx context.Context, req RetrieveRequest) ([]RetrievedChunk, <-chan llm.StreamChunk, error) {
	chunks, err := s.Retrieve(ctx, req)
	if err != nil {
		return nil, nil, fmt.Errorf("retrieving context: %w", err)
	}

	stream, err := s.provider.Stream(ctx, llm.CompletionRequest{
		Messages: buildMessages(req.Query, chunks),
		Model:    s.completionModel,
	})
	if err != nil {
		return chunks, nil, fmt.Errorf("starting completion stream: %w", err)
	}
	return chunks, stream, nil
}

// buildMessages assembles the RAG prompt: a system instruction plus a user
// message carrying the numbered context chunks and the question.
func buildMessages(question string, chunks []RetrievedChunk) []llm.Message {
	var b strings.Builder
	b.WriteString("Context:\n")
	if len(chunks) == 0 {
		b.WriteString("(no relevant context found)\n")
	}
	for i, c := range chunks {
		fmt.Fprintf(&b, "[%d] %s\n", i+1, c.Chunk.Content)
	}
	fmt.Fprintf(&b, "\nQuestion: %s", question)

	return []llm.Message{
		{Role: llm.RoleSystem, Content: systemPrompt},
		{Role: llm.RoleUser, Content: b.String()},
	}
}

// lexicalSearch returns chunk IDs for workspaceID whose content matches
// query's terms, ranked by Postgres full-text relevance (ts_rank) against the
// generated content_tsv column, most relevant first. An empty result (not an
// error) is the normal outcome when query shares no terms with any chunk —
// lexical search is a complement to vector search, not a replacement.
func (s *RetrievalService) lexicalSearch(ctx context.Context, workspaceID uuid.UUID, query string, limit int) ([]uuid.UUID, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id
		FROM chunks
		WHERE workspace_id = $1 AND content_tsv @@ plainto_tsquery('simple', $2)
		ORDER BY ts_rank(content_tsv, plainto_tsquery('simple', $2)) DESC
		LIMIT $3`,
		workspaceID, query, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("querying chunks by full-text search: %w", err)
	}
	defer rows.Close()

	var ids []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scanning chunk id: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating chunk id rows: %w", err)
	}
	return ids, nil
}

// loadChunks batch-fetches chunks by ID. Row order is not guaranteed to match
// ids; callers must re-sort using their own key (e.g. Qdrant score order).
func (s *RetrievalService) loadChunks(ctx context.Context, ids []uuid.UUID) ([]model.Chunk, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, source_id, workspace_id, content, chunk_index, metadata, embedding_model, created_at
		FROM chunks
		WHERE id = ANY($1)`,
		ids,
	)
	if err != nil {
		slog.ErrorContext(ctx, "loading chunks failed", "err", err)
		return nil, fmt.Errorf("querying chunks: %w", err)
	}
	defer rows.Close()

	var chunks []model.Chunk
	for rows.Next() {
		var ch model.Chunk
		var metaBytes []byte
		if err := rows.Scan(
			&ch.ID, &ch.SourceID, &ch.WorkspaceID,
			&ch.Content, &ch.ChunkIndex,
			&metaBytes, &ch.EmbeddingModel, &ch.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scanning chunk row: %w", err)
		}
		ch.Metadata = json.RawMessage(metaBytes)
		chunks = append(chunks, ch)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating chunk rows: %w", err)
	}
	return chunks, nil
}
