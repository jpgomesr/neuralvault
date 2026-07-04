#### ADR-003: Use Qdrant as the vector database for NeuralVault

##### Status
Accepted

##### Context
NeuralVault requires a vector database to store and retrieve semantic embeddings generated from user knowledge sources (Obsidian vaults, Git repositories, PDFs, documentation).
The system ran on a self-hosted Proxmox infrastructure using Docker Compose, with limited resources, at the time this decision was made. **The use of Proxmox is deprecated as of [ADR-004](ADR-004-drop-proxmox-hosting.md)** — this mention is kept only as historical context for the decision below, not as a current infrastructure requirement. The backend is written in Go. The retrieval pipeline requires semantic search as its primary operation, with hybrid search (semantic + keyword) planned for Phase 2.
Four options were evaluated:
- **Qdrant** — purpose-built vector database, written in Rust, self-hosted, official Go client
- **pgvector** — PostgreSQL extension for vector storage, already part of the stack
- **Weaviate** — purpose-built vector database with a larger footprint and Java runtime
- **Pinecone** — managed SaaS vector database, no self-hosting option

##### Decision
Qdrant will be used as the dedicated vector database for NeuralVault.
The decision was made because Qdrant is purpose-built for semantic search, has low memory usage, runs well in Docker, provides an official Go client, and supports native hybrid search — which is a planned Phase 2 requirement. It aligns with the self-hosted infrastructure constraint.
pgvector was considered as a simpler alternative since PostgreSQL is already in the stack, but it is an extension on top of a relational database rather than a dedicated retrieval engine. It remains a valid future migration path if the team wants to reduce infrastructure complexity at scale.
Weaviate was ruled out due to its heavier resource footprint (JVM-based) and less mature Go ecosystem support.
Pinecone was ruled out because it is a managed SaaS product with no self-hosting option, which conflicts with the self-hosted deployment requirement and adds vendor lock-in.

##### Consequences

###### Positive
- Purpose-built for vector retrieval — optimized for semantic search workloads
- Low memory footprint — suitable for self-hosted environments
- Official Go client — integrates cleanly with the Go backend
- Native hybrid search support — no extra work when Phase 2 arrives
- Active development and good documentation
- Consistent with the existing Docker Compose deployment model

###### Negative
- Adds a new infrastructure service alongside PostgreSQL — two databases to operate
- pgvector (already available via PostgreSQL) was not chosen, meaning the simpler path was traded for a dedicated tool
- Team needs to learn the Qdrant API and collection management model


##### Related decisions
- ADR-002 — PostgreSQL was chosen as the primary relational database; it introduced pgvector as a future option, which this decision evaluates and defers in favor of Qdrant
- ADR-004 — deprecates the Proxmox infrastructure mentioned as context above; the Qdrant decision itself is unaffected