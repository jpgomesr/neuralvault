# Getting started

This guide walks you through running NeuralVault locally from scratch.

NeuralVault runs as **infrastructure in Docker Compose** (Postgres, Qdrant, Ollama,
MinIO, Keycloak) plus the **Go API** and **Next.js frontend**, which you run on your
host during development.

---

## Prerequisites

You need the following tools installed before continuing:

| Tool | Version | Install |
| --- | --- | --- |
| Docker + Docker Compose | latest | [docs.docker.com](https://docs.docker.com/get-docker/) |
| Ollama | latest | [ollama.com](https://ollama.com/) |
| Go | 1.26+ | [go.dev/dl](https://go.dev/dl/) |
| Node.js | 20+ | [nodejs.org](https://nodejs.org/) |
| Git | any | [git-scm.com](https://git-scm.com/) |

Docker Compose only starts the backing services — the API and frontend run on your
host — so **Go and Node.js are required** for a local setup.

---

## Step 1 — Clone the repository

```bash
git clone https://github.com/jpgomesr/neuralvault.git
cd neuralvault
```

---

## Step 2 — Pull the embedding model

NeuralVault uses `nomic-embed-text` via Ollama to generate embeddings locally. Pull it before starting:

```bash
ollama pull nomic-embed-text
```

This is a one-time step. The model is approximately 270MB.

If you want to run a local LLM for answers instead of a cloud provider, pull the
model referenced by `OLLAMA_COMPLETION_MODEL` (default `llama3`):

```bash
ollama pull llama3
```

---

## Step 3 — Configure environment variables

There are three env templates. Copy them to their local counterparts:

```bash
cp .env.example .env          # Docker Compose (ports, service credentials)
cp api/.env.example api/.env  # Go API config, read from api/ at startup
cp web/.env.example web/.env.local  # Frontend (optional — defaults work as-is)
```

For a standard local setup **no changes are required** — the defaults line up with
what Docker Compose starts. Key groups in `api/.env` (all `<PREFIX>_<FIELD>`):

```env
SERVER_PORT=8080
POSTGRES_HOST=localhost
QDRANT_URL=localhost
OLLAMA_URL=http://localhost:11434
MINIO_ENDPOINT=localhost:9000

# OIDC — defaults point at the bundled Keycloak dev realm
AUTH_ISSUER_URL=http://localhost:8081/realms/neuralvault
AUTH_CLIENT_ID=neuralvault
AUTH_REDIRECT_URL=http://localhost:8080/auth/callback
AUTH_POST_LOGIN_URL=http://localhost:3000
```

To use a cloud LLM provider instead of Ollama, set its credentials in `api/.env`
(see `api/.env.example` for the available keys).

The frontend needs nothing by default: the browser talks to the API through the
same-origin `/api/*` proxy (`web/next.config.mjs`), which forwards to `API_BASE_URL`
(default `http://localhost:8080`).

---

## Step 4 — Start the infrastructure

```bash
docker compose up -d          # or: make up
```

This starts the backing services (but **not** the API or frontend):

- **PostgreSQL** (`:5432`) — users, workspaces, and metadata
- **Qdrant** (`:6333`) — vector database for semantic search
- **Ollama** (`:11434`) — local embedding and model inference
- **MinIO** (`:9000`, console `:9001`) — object storage for uploaded sources
- **Keycloak** (`:8081`) — OIDC identity provider; the `neuralvault` realm is
  auto-imported with a seeded dev user

Check that all services are healthy:

```bash
docker compose ps
```

---

## Step 5 — Run database migrations

```bash
make migrate-up
```

This applies all pending migrations (`cd api && go run ./cmd/migrate up`) and creates
the schema.

---

## Step 6 — Start the API

In a terminal:

```bash
make run                      # or: cd api && go run ./cmd/server
```

The API listens on [http://localhost:8080](http://localhost:8080).

---

## Step 7 — Start the frontend

In a second terminal:

```bash
cd web
npm install                   # first run only
npm run dev
```

The UI is served at [http://localhost:3000](http://localhost:3000).

---

## Step 8 — Sign in and open the UI

Navigate to [http://localhost:3000](http://localhost:3000). You'll be redirected to
Keycloak to sign in via OIDC. Use the seeded dev user:

| Username | Password |
| --- | --- |
| `dev` | `dev` |

After signing in, create or select a workspace, upload a source, and start chatting.

---

## Connecting your first knowledge source

Once signed in:

1. Create or select a **workspace** from the switcher — every source and query is
   scoped to the active workspace
2. Upload a file as a source
3. Watch the live indexing status until it completes — this may take a few minutes
   depending on the size of the file
4. Ask a question in the chat; the answer streams back grounded in your indexed
   content, with the source chunks it used

---

## Verifying the pipeline

Check the API health endpoint:

```bash
curl http://localhost:8080/health
```

Expected response:

```json
{
  "server": "OK",
  "database": "OK"
}
```

If the API fails to start, check the infrastructure logs:

```bash
docker compose logs qdrant
docker compose logs postgres
docker compose logs ollama
docker compose logs keycloak
```

---

## Stopping NeuralVault

Stop the API and frontend with `Ctrl+C` in their terminals, then stop the
infrastructure:

```bash
docker compose down           # or: make down
```

To also remove all stored data (vectors, database, objects, realm):

```bash
docker compose down -v
```

---

## Updating to a newer version

```bash
git pull
docker compose pull
docker compose up -d
make migrate-up
```

---

## Troubleshooting

**Ollama is not reachable**

Make sure Ollama is running before starting the API:

```bash
ollama serve
```

**The embedding model is missing**

```bash
ollama pull nomic-embed-text
```

**Login fails / Keycloak not ready**

Keycloak takes a few seconds to import the realm on first start. Confirm it's healthy
before signing in:

```bash
docker compose ps keycloak
docker compose logs keycloak
```

**Port conflicts**

If any of `3000` (web), `8080` (api), `5432` (postgres), `6333` (qdrant), `9000`/`9001`
(minio), `8081` (keycloak), or `11434` (ollama) is already in use, change the host port
mapping in `.env` (for the Compose services) or the relevant `SERVER_`/`web` config.

**Migrations fail**

Make sure PostgreSQL is fully started before running migrations:

```bash
docker compose up -d postgres
make migrate-up
```

---

## Next steps

- Read [how it works](docs/how-it-works.md) to understand the full retrieval pipeline
- Read [architecture](docs/architecture.md) for the system design and technology decisions
- Read [contributing](CONTRIBUTING.md) if you want to work on the codebase
