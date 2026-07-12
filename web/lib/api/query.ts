// Thin client over the Go API's /query/stream endpoint.

import type { SourceChunk } from "../types";

interface QueryHandlers {
  onSources?: (chunks: SourceChunk[]) => void;
  onToken?: (text: string) => void;
  onDone?: () => void;
  onError?: (message: string) => void;
}

/**
 * streamQuery POSTs a question to /query/stream and dispatches the SSE events
 * (sources, token, done, error) via handlers. Uses a streaming fetch (not
 * EventSource) so the request can carry a JSON body and the session cookie.
 * This is a multi-token push stream, not a cacheable request/response
 * resource, so it stays outside TanStack Query.
 */
export async function streamQuery(
  workspaceId: string,
  question: string,
  handlers: QueryHandlers,
  conversationId?: string,
  signal?: AbortSignal,
): Promise<void> {
  const res = await fetch("/api/query/stream", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    // conversation_id is dropped from the JSON body when undefined, keeping
    // /query/stream fully stateless for callers that don't pass one.
    body: JSON.stringify({ workspace_id: workspaceId, question, conversation_id: conversationId }),
    signal,
  });
  if (!res.ok || !res.body) {
    handlers.onError?.(`request failed: ${res.status}`);
    return;
  }

  const reader = res.body.getReader();
  const decoder = new TextDecoder();
  let buffer = "";

  for (;;) {
    const { value, done } = await reader.read();
    if (done) break;
    buffer += decoder.decode(value, { stream: true });

    let idx: number;
    while ((idx = buffer.indexOf("\n\n")) !== -1) {
      const block = buffer.slice(0, idx);
      buffer = buffer.slice(idx + 2);
      dispatchSSE(block, handlers);
    }
  }
}

export function dispatchSSE(block: string, handlers: QueryHandlers): void {
  let event = "";
  const dataLines: string[] = [];
  for (const line of block.split("\n")) {
    if (line.startsWith("event:")) event = line.slice(6).trim();
    else if (line.startsWith("data:")) dataLines.push(line.slice(5).trim());
  }
  if (!event) return;

  const data = dataLines.join("\n");
  try {
    switch (event) {
      case "sources":
        handlers.onSources?.(JSON.parse(data).results ?? []);
        break;
      case "token":
        handlers.onToken?.(JSON.parse(data).content ?? "");
        break;
      case "done":
        handlers.onDone?.();
        break;
      case "error":
        handlers.onError?.(JSON.parse(data).error ?? "stream error");
        break;
    }
  } catch {
    handlers.onError?.("malformed stream event");
  }
}
