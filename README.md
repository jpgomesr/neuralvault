# NeuralVault

[![codecov](https://codecov.io/gh/jpgomesr/neuralvault/branch/main/graph/badge.svg)](https://codecov.io/gh/jpgomesr/neuralvault)

> Give AI long-term memory using your notes, repositories, and documentation.

NeuralVault is an open-source AI memory and contextual retrieval platform. Instead of a chatbot that forgets everything the moment the session ends, NeuralVault indexes your knowledge sources — Obsidian vaults, Git repositories, PDFs, docs — and retrieves the most relevant context for every question you ask.

> **Status:** active development — Phase 1 (Foundation). The ingestion pipeline (chunking, object storage, source endpoints, embedding generation, and Qdrant vector storage) is functional. Retrieval engine and frontend are not yet implemented. See the [roadmap](docs/roadmap.md) for the current state.

---

## The problem

AI assistants have no memory of your work. They don't know your projects, your architecture decisions, your notes, or your codebase. Every session starts from zero.

## The solution

NeuralVault connects to your knowledge sources, creates semantic embeddings, and retrieves the right context before sending anything to an LLM. The AI answers with your own knowledge — not generic training data.

---

## Planned features

- **Semantic search** — retrieves by meaning, not exact keywords
- **AI memory** — projects, docs, ADRs, notes, and past fixes persist across sessions
- **Multi-source context** — Obsidian vaults, Git repos, PDFs, and local files in a single session
- **Context optimization** — filters irrelevant chunks, compresses context, reduces token usage
- **Multi-model support** — OpenAI, Claude, Gemini, Ollama (local), Qwen, DeepSeek
- **BYOK** — bring your own API keys; NeuralVault does not resell tokens
- **Fully self-hosted** — runs on Docker Compose, no external services required

---

## How it works

```
Knowledge sources (Obsidian, Git, PDFs, docs)
↓
Chunking engine
↓
Embedding generation (nomic-embed-text via Ollama)
↓
Qdrant vector storage
↓
User query → semantic search → reranking → context compression
↓
Optimized context sent to LLM
↓
Streaming response
```

---

## Stack

| Layer      | Technology              |
| ---------- | ----------------------- |
| Frontend   | Next.js _(planned)_     |
| Backend    | Go + Chi                |
| Vector DB  | Qdrant                  |
| Database   | PostgreSQL              |
| Local AI   | Ollama                  |
| Embeddings | nomic-embed-text        |
| Streaming  | HTTP Streaming / SSE    |
| Analytics  | PostHog _(planned)_     |

For the full folder structure see [CONTRIBUTING.md](CONTRIBUTING.md#project-structure).

---

## Prerequisites

- [Docker](https://docs.docker.com/get-docker/) and Docker Compose
- [Ollama](https://ollama.com/) (for local embeddings and models)
- Go 1.26+ (for local backend development)

---

## Running locally

The API and its dependencies (Qdrant, PostgreSQL, MinIO, Ollama) can be started with Docker Compose. The frontend does not exist yet.

```bash
# Clone the repository
git clone https://github.com/jpgomesr/neuralvault.git
cd neuralvault

# Copy and configure environment variables
cp env.example .env

# Start infrastructure services
docker compose up -d qdrant postgres ollama minio

# Pull the embedding model
ollama pull nomic-embed-text

# Run the API
cd api
go run ./cmd/server
```

The API will be available at `http://localhost:8080`. Swagger docs at `http://localhost:8080/swagger/`.

### Using the CLI

A minimal CLI (`api/cmd/cli`) exercises the pipeline end to end as a plain HTTP client of the API above — no separate setup beyond the server already running.

There's no API to create a workspace yet, so insert one directly for local testing:

```bash
docker compose exec postgres psql -U neuralvault -d neuralvault \
  -c "INSERT INTO workspace (id, name) VALUES (gen_random_uuid(), 'local-dev') RETURNING id;"
```

Then, with `NEURALVAULT_WORKSPACE_ID` set to that UUID (or passed via `--workspace-id` on every call):

```bash
export NEURALVAULT_WORKSPACE_ID=<uuid-from-above>
# NEURALVAULT_API_URL defaults to http://localhost:8080

make run-cli ARGS='ingest README.md'
make run-cli ARGS='query "How does PostgreSQL work?"'
```

`make build-cli` compiles a standalone binary to `dist/nv` (or `dist/neuralvault` if `nv` is already taken on your `PATH`), so you can run `./dist/nv ingest README.md` / `./dist/nv query "..."` directly.

---

## Documentation

| Document | Description |
| --- | --- |
| [Overview](docs/overview.md) | What it is, the problem it solves, and the long-term vision |
| [How it works](docs/how-it-works.md) | Full pipeline: ingestion → chunking → embeddings → retrieval → LLM |
| [Features](docs/features.md) | Semantic search, AI memory, multi-source context, BYOK, and more |
| [Architecture](docs/architecture.md) | Technology stack, system flow diagram, infrastructure setup |
| [Roadmap](docs/roadmap.md) | Development phases and planned features |
| [ADR index](docs/adr/) | Architecture decision records |
| [Spec index](docs/specs/) | Technical specs per system component |

---

## Roadmap

- **Phase 1 — Foundation:** chunking engine, embeddings, Qdrant storage, basic chat, Ollama support
- **Phase 2 — Retrieval quality:** hybrid search, metadata filtering, reranking, dashboard, retrieval analytics
- **Phase 3 — Context intelligence:** context compression, active memory, multi-source retrieval, conversation memory
- **Phase 4 — AI platform:** knowledge graph, intelligent LLM routing, agent memory, cross-workspace retrieval
- **Phase 5 — Ecosystem & DX:** CLI, SDKs, VSCode extension, MCP server, GitHub Action, documentation

See [docs/roadmap.md](docs/roadmap.md) for the full breakdown.

---

## Contributing

Contributions are welcome. Please read [CONTRIBUTING.md](CONTRIBUTING.md) before opening a pull request.

---

## License

NeuralVault is licensed under the [MIT license](LICENSE).