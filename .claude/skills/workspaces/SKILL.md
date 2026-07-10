---
name: workspaces
description: This skill should be used when modifying api/internal/workspaces/ or adding a new workspace-scoped route, or when asked "how is tenant isolation enforced", "why isn't EnsureMember middleware", "add role-based permissions". Documents why the membership guard is a manual function call, not middleware, and that roles exist but aren't enforced yet.
---

# Workspaces: membership guard and role status

## `EnsureMember` is deliberately not middleware

`EnsureMember` (`guard.go`) is a plain function, not chi middleware. Every protected handler in `sources` and `retrieval` calls it explicitly and must `return` immediately if it returns `false`. This is deliberate, not an oversight: `workspace_id` arrives via a different encoding per route — query param on some, path param on others, request body on others — and generic middleware can't extract it consistently across all three without route-specific configuration that would be more complex than the explicit call. When adding a new workspace-scoped route, call `EnsureMember` explicitly the same way existing handlers do; don't assume a middleware layer already covers it.

## Roles exist but nothing enforces them

`model.WorkspaceRole` defines `owner`, `admin`, `member`, and workspace creation atomically makes the creator `owner` in the same transaction as the insert. But `EnsureMember` only checks row *existence* in `user_workspaces` — it never inspects the role column. Every member currently has identical access regardless of role. There is also no invite/add-member/change-role endpoint at all — `POST /workspaces` (create) and `GET /workspaces` (list) are the only routes. Don't assume role-gated behavior exists anywhere in the codebase; if a task requires it, it needs to be built, not just discovered.
