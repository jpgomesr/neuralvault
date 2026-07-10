---
name: sources-ingestion
description: This skill should be used when modifying api/internal/sources/ (upload, ingest, indexing, SSE status) or api/internal/sourcereader/, or when asked "how does ingestion work", "why is a source stuck indexing", "add a new source type". Documents the in-process background indexing model and its concurrency/recovery safeguards.
---

# Sources: in-process indexing and its safeguards

Indexing runs fully in-process (`go s.indexInBackground(...)`) — there is no job queue. This shapes several other design decisions in the package:

## Startup recovery

A server restart mid-index leaves a source stuck in `indexing` status forever, since the goroutine that would finish it is gone. `main.go` calls `sources.ResetStuckIndexing` once at startup specifically to sweep these into `error` status — otherwise an `SSE` client polling `GET /sources/{id}/status` would hang for the full 15-minute stream timeout waiting for a terminal state that will never arrive.

## Re-ingest concurrency guard

`POST /sources/{id}/ingest` uses `claimForIndexing`, a conditional `UPDATE ... WHERE status <> 'indexing'` as an atomic compare-and-swap — this prevents two concurrent ingest requests from racing on the same source's chunk/vector state. A collision returns `ErrAlreadyIndexing`, mapped to HTTP 409. Any new mutation that touches indexing state should use the same claim pattern, not a read-then-write check (which would have the same race the CAS exists to prevent).

## Path normalization

`cleanRelPath` explicitly normalizes `\` to `/` **before** calling `filepath.ToSlash` — `ToSlash` alone is a no-op on Linux, so without the manual normalization, a client sending Windows-style separators to a Linux-hosted server would leave path-traversal-capable `\` sequences intact. Relative paths (normalized this way) double as the stable identifier joining `chunk.Metadata.FilePath` with `source_files.name` across re-ingests — don't switch to an absolute path, it would break that link on every re-run since temp-dir paths change per run.

## Progress streaming

`ProgressBus` (`bus.go`) is an in-memory pub/sub for SSE progress events. Sends are **non-blocking** and silently drop events for slow subscribers — a subscriber that falls behind loses intermediate progress updates, not the connection. This is intentional (progress is best-effort UI feedback, not a guaranteed event log); don't add blocking sends to "fix" dropped events without considering it'll add backpressure to the indexing goroutine itself.

## Adding a new source type

`sourcereader.NewReader`'s dispatch explicitly errors for anything but `SourceTypeFile` (`fmt.Errorf("unsupported source type: %q", ...)`) — this is a real "not implemented" path, not a stub to fill blindly. Note also that `router.go` currently wires a hardcoded `FileReader` and bypasses `NewReader` entirely (see the `go-provider-interface` skill) — adding `git`/`web` support means both implementing the reader **and** switching the router to actually call `NewReader`.
