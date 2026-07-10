---
name: postgres-migrations
description: This skill should be used when adding a database migration, or when asked "add a column/table", "why isn't the schema updated", "how do I run migrations here". Documents that migrations are a fully separate binary from the API server and are never run automatically.
---

# Postgres migrations (goose)

Migrations live in `api/internal/storage/postgres/migrations/` and run through a **separate** binary, `cmd/migrate`, built on `pressly/goose` — bridged from the app's `pgxpool.Pool` to goose's required `*sql.DB` via `stdlib.OpenDBFromPool`.

## `cmd/server` never touches migrations

Starting the API (`go run ./cmd/server`) does not run pending migrations. A fresh database will produce query errors until migrations are applied separately:

```bash
make migrate-up
```

Other targets: `make migrate-down`, `make migrate-status`, `make migrate CMD=<goose-command>` for anything not covered by the named targets.

## Adding a migration

New `.sql` files go under `api/internal/storage/postgres/migrations/`, following goose's naming/format conventions already used by the existing 7 migrations there. After adding one, run `make migrate-up` locally (see the `run` skill for full local-setup order) and verify it applies cleanly before considering the change done — the `verify` skill's "drive the actual flow" step applies here too.
