// Thin client over the Go API. All calls go through the same-origin /api/*
// proxy (see next.config.mjs), so the session cookie is sent automatically.

import type { Me, Source, SourceChunk, SourceProgress, Workspace } from "./types";

/** getMe returns the authenticated user, or null when unauthenticated (401). */
export async function getMe(): Promise<Me | null> {
  const res = await fetch("/api/auth/me");
  if (res.status === 401) return null;
  if (!res.ok) throw new Error(`auth/me failed: ${res.status}`);
  return res.json();
}

export async function logout(): Promise<void> {
  await fetch("/api/auth/logout", { method: "POST" });
}

export async function listWorkspaces(): Promise<Workspace[]> {
  const res = await fetch("/api/workspaces");
  if (!res.ok) throw new Error(`list workspaces failed: ${res.status}`);
  return res.json();
}

export async function createWorkspace(name: string): Promise<Workspace> {
  const res = await fetch("/api/workspaces", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ name }),
  });
  if (!res.ok) throw new Error(`create workspace failed: ${res.status}`);
  return res.json();
}

export async function listSources(workspaceId: string): Promise<Source[]> {
  const res = await fetch(`/api/sources?workspace_id=${encodeURIComponent(workspaceId)}`);
  if (!res.ok) throw new Error(`list sources failed: ${res.status}`);
  return (await res.json()) ?? [];
}

interface UploadResult {
  source: Source;
  status_url: string;
}

export async function uploadSource(
  workspaceId: string,
  name: string,
  files: FileList,
): Promise<UploadResult> {
  const form = new FormData();
  form.append("workspace_id", workspaceId);
  form.append("name", name);
  for (const file of Array.from(files)) form.append("files", file);

  const res = await fetch("/api/sources", { method: "POST", body: form });
  if (!res.ok) throw new Error(`upload failed: ${res.status}`);
  return res.json();
}

/**
 * watchSourceStatus subscribes to a source's indexing progress via SSE. The
 * returned EventSource is closed automatically on a terminal event; the caller
 * may also close it early to unsubscribe.
 */
export function watchSourceStatus(
  sourceId: string,
  handlers: {
    onProgress?: (ev: SourceProgress) => void;
    onDone?: (ev: SourceProgress) => void;
    onError?: (ev: SourceProgress) => void;
  },
): EventSource {
  const es = new EventSource(`/api/sources/${sourceId}/status`);
  es.onmessage = (e) => {
    const ev = JSON.parse(e.data) as SourceProgress;
    if (ev.type === "done") {
      handlers.onDone?.(ev);
      es.close();
    } else if (ev.type === "error") {
      handlers.onError?.(ev);
      es.close();
    } else {
      handlers.onProgress?.(ev);
    }
  };
  es.onerror = () => {
    handlers.onError?.({ type: "error", error: "connection lost" });
    es.close();
  };
  return es;
}

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
 */
export async function streamQuery(
  workspaceId: string,
  question: string,
  handlers: QueryHandlers,
  signal?: AbortSignal,
): Promise<void> {
  const res = await fetch("/api/query/stream", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ workspace_id: workspaceId, question }),
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
