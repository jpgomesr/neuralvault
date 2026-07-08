// Thin client over the Go API's source endpoints.

import type { Source, SourceFile, SourceProgress } from "../types";

export async function listSources(workspaceId: string): Promise<Source[]> {
  const res = await fetch(`/api/sources?workspace_id=${encodeURIComponent(workspaceId)}`);
  if (!res.ok) throw new Error(`list sources failed: ${res.status}`);
  return (await res.json()) ?? [];
}

/** listSourceFiles returns the original files stored for a source. */
export async function listSourceFiles(sourceId: string): Promise<SourceFile[]> {
  const res = await fetch(`/api/sources/${encodeURIComponent(sourceId)}/files`);
  if (!res.ok) throw new Error(`list source files failed: ${res.status}`);
  return (await res.json()) ?? [];
}

/**
 * sourceFileContentUrl builds the URL that streams a file's content. Suitable as
 * an <img>/<iframe> src or a download link href; the API sets the content type.
 */
export function sourceFileContentUrl(sourceId: string, path: string): string {
  return `/api/sources/${encodeURIComponent(sourceId)}/files/content?path=${encodeURIComponent(path)}`;
}

/** fetchSourceFileText fetches a file's content as text (for text/markdown preview). */
export async function fetchSourceFileText(sourceId: string, path: string): Promise<string> {
  const res = await fetch(sourceFileContentUrl(sourceId, path));
  if (!res.ok) throw new Error(`fetch file failed: ${res.status}`);
  return res.text();
}

export interface UploadResult {
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
 * may also close it early to unsubscribe. This is a push-based stream, not a
 * cacheable request/response resource, so it stays outside TanStack Query —
 * callers invalidate the sources query themselves on "done".
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
