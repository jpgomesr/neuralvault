---
name: retrieval
description: This skill should be used when modifying api/internal/retrieval/ (query, streaming answers, chunk hydration), or when asked "how does retrieval work", "why re-filter after Qdrant already filtered", "add a new caller of loadChunks". Documents three non-obvious invariants in the retrieval pipeline that look redundant or arbitrary but aren't.
---

# Retrieval pipeline invariants

## Defense-in-depth workspace filtering

`Retrieve` re-filters Qdrant results by `chunk.WorkspaceID == req.WorkspaceID` in Go, even though the Qdrant query already filters by the `workspace_id` payload field server-side. This is documented defense-in-depth, not redundant code to delete — it protects against a payload/query mismatch bug or a future change to the Qdrant filter logic silently leaking cross-workspace chunks. Keep both filters if touching this path.

## Hydration order is not query order

Postgres chunk hydration (`WHERE id = ANY($1)`) does **not** preserve Qdrant's ranked order — SQL gives no ordering guarantee for `ANY()`. The code re-sorts hydrated rows using a `rank` map built from the original Qdrant response order. Any new caller of `loadChunks` must apply the same re-sort — reusing the raw hydration result directly will silently return chunks in the wrong relevance order.

## topK is clamped, not validated

`topK` silently clamps to `[1, 50]` rather than returning an error for out-of-range input. If a caller needs to know whether their requested `topK` was honored as-is, they must compare the request value to the response themselves — the retrieval layer won't tell them it was clamped.
