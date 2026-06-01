# Getting started

This guide walks you through running NeuralVault locally from scratch.

---

## Prerequisites

You need the following tools installed before continuing:

| Tool | Version | Install |
| --- | --- | --- |
| Docker + Docker Compose | latest | [docs.docker.com](https://docs.docker.com/get-docker/) |
| Ollama | latest | [ollama.com](https://ollama.com/) |
| Git | any | [git-scm.com](https://git-scm.com/) |

Go and Node.js are only required if you want to run the backend or frontend outside of Docker. For a standard local setup, Docker is enough.

---

## Step 1 — Clone the repository

```bash
git clone https://github.com/jpgomesr/NeuralVault.git
cd NeuralVault
```

---

## Step 2 — Pull the embedding model

NeuralVault uses `nomic-embed-text` via Ollama to generate embeddings locally. Pull it before starting:

```bash
ollama pull nomic-embed-text
```

This is a one-time step. The model is approximately 270MB.

If you also want to run a local LLM instead of using a cloud provider, pull one now:

```bash
# Example — pick any model Ollama supports
ollama pull llama3
```

---

## Step 3 — Configure environment variables

Copy the example environment file:

```bash
cp .env.example .env
```

Open `.env` and review the defaults. For a standard local setup, no changes are required. The defaults are:

```env
# API
DATABASE_URL=postgres://postgres:postgres@localhost:5432/neuralvault
QDRANT_URL=http://localhost:6333
OLLAMA_URL=http://localhost:11434

# Frontend
NEXT_PUBLIC_API_URL=http://localhost:8080
```

If you want to use a cloud LLM provider, add your API key:

```env
# Add only the provider you want to use
OPENAI_API_KEY=sk-...
ANTHROPIC_API_KEY=sk-ant-...
GOOGLE_API_KEY=...
```

---

## Step 4 — Start the services

```bash
docker compose up -d
```

This starts:

- **PostgreSQL** — relational database for users, workspaces, and metadata
- **Qdrant** — vector database for semantic search
- **Ollama** — local embedding and model inference
- **NeuralVault API** — Go backend on port `8080`
- **NeuralVault UI** — Next.js frontend on port `3000`

Check that all services are running:

```bash
docker compose ps
```

All services should show status `running`.

---

## Step 5 — Run database migrations

```bash
docker compose exec api make migrate
```

This creates the initial database schema.

---

## Step 6 — Open the UI

Navigate to [http://localhost:3000](http://localhost:3000) in your browser.

Create an account, connect a knowledge source, and start chatting.

---

## Connecting your first knowledge source

Once logged in:

1. Go to **Sources** in the sidebar
2. Click **Add source**
3. Choose a source type — Obsidian vault, Git repository, PDF, or local folder
4. Follow the connection steps for that source type
5. Wait for indexing to complete — this may take a few minutes depending on the size of your source
6. Open **Chat** and ask a question about your content

---

## Verifying the pipeline

To confirm that embeddings and retrieval are working correctly, check the API health endpoint:

```bash
curl http://localhost:8080/health
```

Expected response:

```json
{
  "status": "ok",
  "qdrant": "ok",
  "postgres": "ok",
  "ollama": "ok"
}
```

If any service shows a status other than `ok`, check its logs:

```bash
docker compose logs qdrant
docker compose logs postgres
docker compose logs ollama
docker compose logs api
```

---

## Stopping NeuralVault

```bash
docker compose down
```

To also remove all stored data (vectors, database):

```bash
docker compose down -v
```

---

## Updating to a newer version

```bash
git pull
docker compose pull
docker compose up -d
docker compose exec api make migrate
```

---

## Troubleshooting

**Ollama is not reachable**

Make sure Ollama is running before starting the stack:

```bash
ollama serve
docker compose up -d
```

**The embedding model is missing**

```bash
ollama pull nomic-embed-text
```

**Port conflicts**

If port `3000`, `8080`, `5432`, or `6333` is already in use on your machine, edit `docker-compose.yml` and change the host port mapping for the conflicting service.

**Migrations fail**

Make sure PostgreSQL is fully started before running migrations:

```bash
docker compose up -d postgres
sleep 3
docker compose exec api make migrate
```

---

## Next steps

- Read [how it works](how-it-works.md) to understand the full retrieval pipeline
- Read [architecture](architecture.md) for the system design and technology decisions
- Read [contributing](../CONTRIBUTING.md) if you want to work on the codebase