package sources

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/google/uuid"

	"github.com/jpgomesr/NeuralVault/internal/llm"
	"github.com/jpgomesr/NeuralVault/internal/model"
)

// captionPrompt asks the LLM to describe a fenced code block or table in
// prose. Fenced diagrams (box-drawing characters) and Markdown tables
// (pipe/dash formatting) carry little semantic signal for embedding or
// reranking on their own — a caption naming the actual components and
// relationships gives both a real natural-language anchor to work with.
const captionPrompt = "The following is an excerpt from a technical document containing a diagram or table. " +
	"Describe what it shows in one or two plain sentences, naming the specific components and how they relate. " +
	"Respond with only the description, nothing else."

// captionStructuredChunks appends an LLM-generated caption to any chunk whose
// content contains a fenced code block or a Markdown table, updating both the
// persisted chunk row and the in-memory copy used for embedding — so the
// caption is stored, displayed, lexically searchable, and embedded
// consistently. A captioning failure for one chunk is logged and skipped;
// captions are an enrichment, not a correctness requirement, so one failure
// must not fail the whole ingest.
func (s *SourceService) captionStructuredChunks(ctx context.Context, chunks []model.Chunk, models ingestModels) []model.Chunk {
	for i, ch := range chunks {
		if !hasStructuredContent(ch.Content) {
			continue
		}

		caption, err := s.generateCaption(ctx, ch.Content, models)
		if err != nil {
			slog.WarnContext(ctx, "chunk captioning failed, continuing without caption", "err", err, "chunk_id", ch.ID)
			continue
		}
		if caption == "" {
			continue
		}

		newContent := ch.Content + "\n\n" + caption
		if err := s.updateChunkContent(ctx, ch.ID, newContent); err != nil {
			slog.WarnContext(ctx, "persisting chunk caption failed, continuing without caption", "err", err, "chunk_id", ch.ID)
			continue
		}
		chunks[i].Content = newContent
	}
	return chunks
}

// generateCaption asks the workspace's configured LLM to describe content in prose.
func (s *SourceService) generateCaption(ctx context.Context, content string, models ingestModels) (string, error) {
	resp, err := models.provider.Complete(ctx, llm.CompletionRequest{
		Model:    models.model,
		Messages: []llm.Message{{Role: llm.RoleUser, Content: captionPrompt + "\n\n" + content}},
	})
	if err != nil {
		return "", fmt.Errorf("generating caption: %w", err)
	}
	return strings.TrimSpace(resp.Content), nil
}

// updateChunkContent overwrites a persisted chunk's content column. Used only
// to append a caption after the chunk has already been inserted by
// chunking.ChunkService — the generated content_tsv full-text column
// recomputes automatically from the new value.
func (s *SourceService) updateChunkContent(ctx context.Context, id uuid.UUID, content string) error {
	if _, err := s.pool.Exec(ctx, `UPDATE chunks SET content = $1 WHERE id = $2`, content, id); err != nil {
		return fmt.Errorf("updating chunk content: %w", err)
	}
	return nil
}

// hasStructuredContent reports whether content contains a fenced code block
// or what looks like a Markdown table — a cheap, sufficient trigger for
// whether captioning is worth an LLM call. It doesn't need to extract exact
// block boundaries, since the whole chunk is sent to the LLM for context.
func hasStructuredContent(content string) bool {
	if strings.Contains(content, "```") {
		return true
	}

	pipeLines := 0
	for _, line := range strings.Split(content, "\n") {
		t := strings.TrimSpace(line)
		if strings.HasPrefix(t, "|") && strings.HasSuffix(t, "|") && strings.Count(t, "|") >= 2 {
			pipeLines++
			if pipeLines >= 2 {
				return true
			}
		}
	}
	return false
}
