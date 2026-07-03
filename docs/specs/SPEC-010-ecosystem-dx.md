#### SPEC-010: Ecosystem and developer experience

##### Status
Draft

##### Problem statement
NeuralVault's value grows with the number of places it can be reached from: terminals, editors, agents, CI. Roadmap Phase 5 ("Ecosystem & Developer Experience") plans the integration surface — CLI, SDKs, VSCode extension, MCP server, GitHub Action, documentation. This is the furthest-out area; this spec frames intent and constraints so the platform API evolves in a direction these consumers can build on.

##### Goals
- CLI: manage sources and query the vault from the terminal against a self-hosted instance.
- SDKs: thin typed clients over the HTTP API for programmatic ingestion and retrieval.
- MCP server: expose retrieval (and possibly ingestion) as MCP tools so agents like Claude Code can use a NeuralVault instance as a memory backend.
- VSCode extension and GitHub Action: bring vault context into the editor and index repositories from CI.

##### Non-goals
- New platform capabilities — every ecosystem tool is a client of the existing HTTP API; anything a tool needs that the API lacks becomes a platform requirement first (e.g. in [SPEC-006](SPEC-006-retrieval-engine.md)).
- Hosted/multi-instance distribution concerns — NeuralVault remains self-hosted; tools point at a user's own instance.

##### Proposed design
Intentionally high-level until Phase 5 approaches:

- All tools consume the public HTTP API; the Swagger spec (already CI-enforced, see [SPEC-009](SPEC-009-platform-cross-cutting.md)) is the source of truth and a candidate input for SDK generation.
- The CLI would live alongside the API in this repo (Go, natural fit with `api/`); SDK languages and repository layout are open.
- The MCP server maps API operations to tools (e.g. `search_vault`, `add_source`) and depends on retrieval ([SPEC-006](SPEC-006-retrieval-engine.md)) being exposed as a first-class endpoint.

##### Affected components
- New top-level directories/repos (CLI, SDKs, MCP server, extension) — none exist yet
- The HTTP API surface (`api/internal/*/routes.go`) as the contract all tools consume

##### Open questions
- Which comes first? An MCP server arguably delivers the most differentiated value (agent memory) with the smallest surface.
- API authentication for non-browser clients (API tokens?) — depends on the auth design in [SPEC-009](SPEC-009-platform-cross-cutting.md).
- Are SDKs generated from the Swagger spec or hand-written, and which languages lead?
- Monorepo vs. separate repos for ecosystem tools?

##### Acceptance criteria
- Every ecosystem tool operates exclusively through the documented HTTP API — no private hooks into internal packages.
- At least one end-to-end path exists per shipped tool (e.g. CLI: add a source, wait for indexing, query it; MCP: an agent retrieves workspace context via a tool call).

##### Related (Optional)
- [SPEC-006](SPEC-006-retrieval-engine.md), [SPEC-009](SPEC-009-platform-cross-cutting.md)
