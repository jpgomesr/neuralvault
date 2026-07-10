---
name: web-conventions
description: This skill should be used when adding a new API call, hook, or component under web/, or when asked "how do I fetch data in the frontend here", "where does this API call go", "add a new page". Documents the fetch-wrapper -> TanStack Query hook -> component layering, which is real and consistent in the codebase but not written down in web/README.md.
---

# Frontend data-fetching layering

`web/` follows a consistent three-layer pattern for any new piece of server data, confirmed by reading `lib/api/sources.ts` alongside `hooks/use-sources.ts` — but it isn't documented anywhere, so state it explicitly rather than improvising a different shape.

## 1. `lib/api/<domain>.ts` — typed fetch wrapper

A thin function per operation that calls `fetch`, throws on `!res.ok`, and returns data typed from `lib/types.ts`. Each file gets a matching `<domain>.test.ts`; shared test setup lives in `lib/api/test-helpers.ts`.

## 2. `hooks/use-<domain>.ts` — TanStack Query hook

Wrap the fetch function in a hook:
- Reads: `useQuery`, keyed via a `<domain>QueryKey` factory function (not an inline array) so invalidation elsewhere can reference the same key
- Writes: `useMutation`, invalidating the corresponding read query key `onSuccess`

## 3. `components/` — consume the hook

Components call the hook, never `lib/api/*` directly. New shadcn primitives go under `components/ui/`, added via the shadcn CLI per `components.json` (style `radix-nova`, aliases `@/components`, `@/lib`, `@/hooks`, `@/components/ui`) — not hand-written from scratch.

## Exception: SSE stays outside TanStack Query

`watchSourceStatus` (`lib/api/sources.ts`) is push-based (Server-Sent Events), not a cacheable request/response resource, so it deliberately does **not** go through a `useQuery` hook. Callers subscribe directly and invalidate the relevant query manually when a terminal SSE event arrives. Don't force a new SSE-based feature into the `useQuery` pattern — follow `watchSourceStatus`'s shape instead.
