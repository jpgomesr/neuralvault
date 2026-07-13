---
name: retrieval
description: This skill should be used when modifying api/internal/retrieval/ (query, streaming answers, chunk hydration), or when asked "how does retrieval work", "why re-filter after Qdrant already filtered", "add a new caller of loadChunks", "why does Retrieve query Postgres full-text too". Documents four non-obvious invariants in the retrieval pipeline that look redundant or arbitrary but aren't.
---

# Retrieval pipeline invariants

## Defense-in-depth workspace filtering

`Retrieve` re-filters Qdrant results by `chunk.WorkspaceID == req.WorkspaceID` in Go, even though the Qdrant query already filters by the `workspace_id` payload field server-side (and `lexicalSearch`'s SQL already scopes by `workspace_id` too). This is documented defense-in-depth, not redundant code to delete — it protects against a payload/query mismatch bug or a future change to either filter silently leaking cross-workspace chunks. Keep all filters if touching this path.

## Hydration order is not query order

Postgres chunk hydration (`WHERE id = ANY($1)`) does **not** preserve Qdrant's ranked order — SQL gives no ordering guarantee for `ANY()`. The code re-sorts hydrated rows using a `fused` score map (see below) built from the original candidate lists' order. Any new caller of `loadChunks` must apply its own re-sort — reusing the raw hydration result directly will silently return chunks in the wrong relevance order.

## topK is clamped, not validated

`topK` silently clamps to `[1, 50]` rather than returning an error for out-of-range input. If a caller needs to know whether their requested `topK` was honored as-is, they must compare the request value to the response themselves — the retrieval layer won't tell them it was clamped.

## Retrieve fuses vector search with Postgres full-text search

`Retrieve` is not pure vector search: it also runs `lexicalSearch`, a Postgres full-text query against the generated `chunks.content_tsv` column (`'simple'` text-search config — deliberately no stemming, since the goal is catching literal technical terms regardless of query language, not English grammar). Both the Qdrant vector search and `lexicalSearch` are widened to a `candidatePoolSize(topK)` pool (wider than the final `topK`), then combined via Reciprocal Rank Fusion (`rrfK = 60`) before truncating to `topK`. This exists because pure cosine similarity systematically under-ranks structurally "dense" content (tables, diagrams) relative to short generic prose, even when the dense content literally contains the query's terms — confirmed empirically before this was added. `RetrievedChunk.Score` still reports the chunk's vector cosine similarity (0 if it was a lexical-only match), not the RRF fusion score — RRF is an internal ranking signal on a different, much smaller scale, not something to surface as a citation "confidence" number.
