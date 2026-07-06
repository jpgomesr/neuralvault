# NeuralVault

[![codecov](https://codecov.io/gh/jpgomesr/neuralvault/branch/main/graph/badge.svg)](https://codecov.io/gh/jpgomesr/neuralvault)

> Give AI long-term memory using your notes, repositories, and documentation.

NeuralVault is an open-source AI memory and contextual retrieval platform. Instead of a chatbot that forgets everything the moment the session ends, NeuralVault indexes your knowledge sources — Obsidian vaults, Git repositories, PDFs, docs — and retrieves the most relevant context for every question you ask.

> **Status:** active development — Phase 1 (Foundation) complete. Ingestion pipeline, retrieval engine with streaming grounded answers, OIDC authentication with workspace-scoped tenant isolation, and the Next.js chat UI are all functional. See the [roadmap](docs/roadmap.md) for what's next.

---

## The problem

AI assistants have no memory of your work. They don't know your projects, your architecture decisions, your notes, or your codebase. Every session starts from zero.

## The solution

NeuralVault connects to your knowledge sources, creates semantic embeddings, and retrieves the right context before sending anything to an LLM. The AI answers with your own knowledge — not generic training data.

---

## Features

- **Semantic search** — retrieves by meaning, not exact keywords
- **AI memory** — indexed knowledge persists across sessions in the vector database
- **Workspaces** — isolated, membership-guarded knowledge bases per team or project
- **Streaming answers** — grounded responses stream back with the source chunks used
- **Fully self-hosted** — infrastructure on Docker Compose, no external services required
- **Multi-source context** _(planned)_ — Obsidian vaults, Git repos, PDFs, and local files in a single session
- **Context optimization** _(planned)_ — filters irrelevant chunks, compresses context, reduces token usage
- **Multi-model support** — Ollama (local) today; OpenAI, Claude, Gemini, Qwen, DeepSeek _(planned)_
- **BYOK** _(planned)_ — bring your own API keys; NeuralVault does not resell tokens

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
| Frontend   | Next.js (App Router)    |
| Backend    | Go + Chi                |
| Vector DB  | Qdrant                  |
| Database   | PostgreSQL              |
| Object storage | MinIO               |
| Auth       | OIDC (Keycloak in dev)  |
| Local AI   | Ollama                  |
| Embeddings | nomic-embed-text        |
| Streaming  | HTTP Streaming / SSE    |
| Analytics  | PostHog _(planned)_     |

For the full folder structure see [CONTRIBUTING.md](CONTRIBUTING.md#project-structure).

---

## Prerequisites

- [Docker](https://docs.docker.com/get-docker/) and Docker Compose
- [Ollama](https://ollama.com/) (for local embeddings and models)
- Go 1.26+ (backend)
- Node.js 20+ (frontend)

---

## Running locally

Infrastructure (Postgres, Qdrant, Ollama, MinIO, Keycloak) runs in Docker Compose; the API and frontend run on your host. Full walkthrough in [getting-started.md](getting-started.md) — short version:

```bash
git clone https://github.com/jpgomesr/neuralvault.git
cd neuralvault

# Environment templates (defaults work as-is)
cp .env.example .env          # Docker Compose (ports, service credentials)
cp api/.env.example api/.env  # Go API config

# Infrastructure + embedding model
docker compose up -d
ollama pull nomic-embed-text

# Migrations, API, frontend
make migrate-up
make run                              # API at :8080 (Swagger at /swagger/)
cd web && npm install && npm run dev  # UI at :3000
```

Open [http://localhost:3000](http://localhost:3000) and sign in via the bundled Keycloak dev realm (user `dev`, password `dev`).

### Using the CLI

A minimal CLI (`api/cmd/cli`) exercises ingest and query as a plain HTTP client of the API (`make run-cli ARGS='...'`, or `make build-cli` for a standalone `dist/nv` binary).

> **Note:** the CLI predates authentication. `/sources` and `/query` now require a session cookie, and the CLI has no login flow yet — how the CLI authenticates is an open question in [SPEC-011](docs/specs/SPEC-011-auth-workspaces-tenant-isolation.md), so it is currently not usable against a default setup.

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

- **Phase 1 — Foundation** _(complete)_**:** chunking engine, embeddings, Qdrant storage, auth + workspaces, chat interface, Ollama support
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