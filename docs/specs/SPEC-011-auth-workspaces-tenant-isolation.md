#### SPEC-011: Authentication, workspace management, and tenant isolation

##### Status
Draft

##### Problem statement
The data model is multi-tenant from day one (`users`, `workspace`, `user_workspace`, `user_identity` — [SPEC-005](SPEC-005-relational-persistence.md)), and every ingestion and retrieval operation is workspace-scoped. But nothing enforces any of it: `workspace_id` is client-supplied on every request (`POST /query` body, `POST /sources` form field), no endpoint creates workspaces or users, and no middleware establishes who the caller is. Any caller can read or write any workspace. [SPEC-009](SPEC-009-platform-cross-cutting.md) records this as an acknowledged gap; roadmap Phase 1 now lists Auth, Workspace management, and Workspace membership middleware. This spec designs how identity is established, how workspaces are created and joined, and where tenant isolation is enforced.

##### Goals
- Establish caller identity on every request via an auth middleware, backed by external identity providers through `user_identity` (no first-party password storage — the schema has none by design).
- A `workspaces` domain: create a workspace (creator becomes `owner` in `user_workspace`) and list the authenticated user's workspaces — giving the frontend a real way to obtain a `workspace_id`.
- Enforce workspace membership on every `workspace_id`-scoped route (`/sources`, `/query`, and future chat), so a valid identity cannot touch a workspace it doesn't belong to.
- Just-in-time user provisioning: first login through a provider creates the `users` row and its `user_identity` link atomically.

##### Non-goals
- First-party credentials (password hashing, reset flows) — `user_identity` models external providers only.
- Role-based permission granularity beyond membership — `owner`/`admin`/`member` exist in the schema, but per-role authorization rules (who can delete sources, invite members) are deferred until the operations that need them exist.
- API tokens for non-browser clients (CLI, SDKs, MCP — [SPEC-010](SPEC-010-ecosystem-dx.md)) — designed later on top of the same middleware; the CLI keeps working against trusted local instances meanwhile.
- Postgres row-level security — isolation is enforced in the application layer for now (see Open questions in [SPEC-005](SPEC-005-relational-persistence.md)).

##### Proposed design
Two new domains following the repo's handler/service/routes layout, plus middleware in `router/router.go`:

- **`api/internal/auth/`** — the login flow: OAuth-style redirect/callback endpoints per provider. On callback, the service looks up `user_identity` by `(provider, external_id)`; if absent, it creates `users` + `user_identity` in one transaction (JIT provisioning). It then issues the session credential and exposes middleware `auth.RequireUser` that resolves the credential to a `user_id` and stores it in the request context (pattern mirrors `logger.RequestID`).
- **`api/internal/workspaces/`** — `POST /workspaces` (creates `workspace` + `user_workspace` row with role `owner` transactionally) and `GET /workspaces` (lists workspaces joined through `user_workspace` for the context's `user_id`, using the existing `idx_user_workspace_workspace_id` access paths). Mounted in `router.go` like every other domain.
- **Membership enforcement** — `auth.RequireWorkspace` middleware (or a shared service check) runs after `RequireUser` on workspace-scoped routes: it reads the request's `workspace_id` and verifies a `user_workspace` row exists for `(user_id, workspace_id)`, rejecting with `403` otherwise. Handlers keep receiving `workspace_id` explicitly as today; the middleware only vouches that the pair is legitimate.
- **Route wiring** — `/health` and `/swagger` stay public; `/sources`, `/query`, `/workspaces` are wrapped by `RequireUser`, and the first two additionally by the membership check.

##### Affected components
- `api/internal/auth/` (new), `api/internal/workspaces/` (new)
- `api/internal/router/router.go` — middleware ordering and route wiring
- `api/internal/config/` — provider credentials/config (`AUTH_`-prefixed, following the existing prefix convention)
- `api/internal/model/` — `User`, `Workspace`, `UserWorkspace`, `UserIdentity` already exist; no schema change expected
- `docs/roadmap.md` Phase 1 items: Auth, Workspace management, Workspace membership middleware

##### Open questions
- Session mechanism: server-side sessions (needs a store — Redis is "future" in `docs/architecture.md`) vs. stateless JWT (revocation story?). This is an ADR-worthy decision.
- Which identity providers launch first (Google? GitHub?), and does a dev-mode bypass exist for local single-user setups where running an OAuth app is friction?
- Enforcement point: is membership checked once in middleware (needs the `workspace_id` extracted before the handler — it lives in body/form/query today, in three different shapes) or per-service? Middleware may push `workspace_id` into the path (`/workspaces/{id}/sources`), which is a breaking API reshape.
- Do SSE endpoints (`GET /sources/{id}/status`) authenticate the same way, given `EventSource` cannot set headers?
- How does the CLI ([SPEC-010](SPEC-010-ecosystem-dx.md)) authenticate before API tokens exist?

##### Acceptance criteria
- An unauthenticated request to `/sources`, `/query`, or `/workspaces` receives `401`; `/health` and `/swagger` remain public.
- First login through a provider creates exactly one `users` row and one `user_identity` row; subsequent logins reuse them.
- `POST /workspaces` returns a workspace whose creator holds an `owner` row in `user_workspace`.
- An authenticated user querying a workspace they don't belong to receives `403`, and no cross-workspace data is returned.

##### Related (Optional)
- [SPEC-005](SPEC-005-relational-persistence.md) — the schema this spec activates
- [SPEC-009](SPEC-009-platform-cross-cutting.md) — where the gap is recorded
- [SPEC-010](SPEC-010-ecosystem-dx.md) — non-browser client auth builds on this
- [SPEC-007](SPEC-007-llm-provider-layer.md) — BYOK key scoping depends on identity
