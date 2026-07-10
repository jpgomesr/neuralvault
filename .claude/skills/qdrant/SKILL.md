---
name: qdrant
description: This skill should be used when modifying api/internal/vectorstorage/, or when asked "how is the vector store abstracted here", "why does the interface use Qdrant types directly", "swap the vector database". Documents that the Client interface is intentionally typed against Qdrant's own protobuf types, not neutral domain types.
---

# Qdrant client: an intentionally leaky interface

`vectorstorage.Client` (`vectorstorage.go`) is typed directly against Qdrant's own protobuf types (`qdrantpb.*`) rather than backend-neutral domain types the way `embedding.Embedder` or `llm.Provider` are. This is a documented decision (see [ADR-008](../../../docs/adr/ADR-008-storage-vectorstorage-interface-abstraction.md)), not an inconsistency to "fix" by introducing neutral types — the interface's own doc comment states that swapping the implementation only works "without changing this interface if the types remain compatible." Treat this package's abstraction as intentionally leakier than the other provider interfaces, and don't assume the same neutral-types pattern used in `embedding`/`llm` applies here.

## Startup behavior

`vectorstorage/qdrant`'s `New*` constructor runs `ensureCollection`, which auto-creates the collection with cosine distance if it doesn't already exist — same fail-fast-at-construction pattern documented in the `go-provider-interface` skill (shared with `objectstorage/minio`'s `ensureBucket` and both Ollama clients' model checks).
