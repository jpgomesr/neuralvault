---
name: ollama
description: This skill should be used when modifying api/internal/embedding/ollama/ or api/internal/llm/ollama/, or when asked "why did the server fail to start over Ollama", "how does streaming work here", "add timeout handling to the LLM client". Documents the fail-fast model check and why the LLM client has no HTTP timeout.
---

# Ollama clients: fail-fast startup and streaming

## Fail-fast model check

Both the embedding client (`embedding/ollama`) and the LLM client (`llm/ollama`) call Ollama's `/api/tags` at construction time and fail immediately if the configured model isn't pulled. This means a missing `ollama pull nomic-embed-text` (or the configured `OLLAMA_COMPLETION_MODEL`) breaks **server startup**, not the first request. When debugging a server that won't start, check this before assuming a code regression.

## No HTTP client timeout on the LLM client — deliberately

`llm/ollama`'s `http.Client` has no `Timeout` set, unlike a typical Go HTTP client setup. This is intentional: `http.Client.Timeout` bounds the entire request including body read, and a long streaming completion would be truncated mid-stream by any fixed timeout. Duration is bounded by the caller's `ctx` instead. Do not add a `Timeout` back to this client without switching streaming callers to a context-based cancellation that doesn't conflict with in-flight token streaming.

## Streaming error surface

`Stream` issues and status-checks the HTTP request **synchronously** — a bad model or unreachable Ollama surfaces as a normal returned `error` from `Stream` itself. Once the request succeeds, the response body is handed to a goroutine; any error that occurs *after* that point (e.g. a connection drop mid-stream) arrives as a `StreamChunk{Error: ..., Done: true}` value sent on the channel, not as a returned Go `error`. Callers must check `StreamChunk.Error` on every received chunk, not just handle `Stream`'s initial return value.
