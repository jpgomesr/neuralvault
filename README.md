# NeuralVault

> Give AI long-term memory using your notes, repositories, and documentation.

NeuralVault is an open-source AI memory and contextual retrieval platform. Instead of a chatbot that forgets everything the moment the session ends, NeuralVault indexes your knowledge sources — Obsidian vaults, Git repositories, PDFs, docs — and retrieves the most relevant context for every question you ask.

---

## The problem

AI assistants have no memory of your work. They don't know your projects, your architecture decisions, your notes, or your codebase. Every session starts from zero.

## The solution

NeuralVault connects to your knowledge sources, creates semantic embeddings, and retrieves the right context before sending anything to an LLM. The AI answers with your own knowledge — not generic training data.

---

## Features

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

| Layer      | Technology           |
| ---------- | -------------------- |
| Frontend   | Next.js              |
| Backend    | Go + Chi             |
| Vector DB  | Qdrant               |
| Database   | PostgreSQL           |
| Local AI   | Ollama               |
| Embeddings | nomic-embed-text     |
| Streaming  | HTTP Streaming / SSE |
| Analytics  | PostHog              |

---

## Prerequisites

- [Docker](https://docs.docker.com/get-docker/) and Docker Compose
- [Ollama](https://ollama.com/) (for local embeddings and models)
- Go 1.22+ (for local backend development)
- Node.js 20+ (for local frontend development)

---

## Quickstart

```bash
# Clone the repository
git clone https://github.com/jpgomesr/NeuralVault.git
cd NeuralVault

# Pull the embedding model
ollama pull nomic-embed-text

# Start all services
docker compose up -d

# NeuralVault is now running at http://localhost:3000
```

> A full setup guide with configuration options is available in [docs/getting-started.md](docs/getting-started.md).

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

---

## Roadmap

- **Phase 1 — Foundation:** chunking engine, embeddings, Qdrant storage, basic chat, Ollama support
- **Phase 2 — Retrieval quality:** hybrid search, reranking, dashboard, retrieval analytics
- **Phase 3 — Context intelligence:** context compression, active memory, multi-source retrieval
- **Phase 4 — Developer tooling:** VSCode extension, AI coding memory, knowledge graph

See [docs/roadmap.md](docs/roadmap.md) for the full breakdown.

---

## Contributing

Contributions are welcome. Please read [CONTRIBUTING.md](CONTRIBUTING.md) before opening a pull request.

---

## License

NeuralVault is licensed under the [MIT license](LICENSE).