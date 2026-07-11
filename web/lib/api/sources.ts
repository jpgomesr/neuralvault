// Thin client over the Go API's source endpoints.

import type { Source, SourceFile, SourceProgress } from "../types";

export async function listSources(workspaceId: string): Promise<Source[]> {
  const res = await fetch(`/api/sources?workspace_id=${encodeURIComponent(workspaceId)}`);
  if (!res.ok) throw new Error(`list sources failed: ${res.status}`);
  return (await res.json()) ?? [];
}

/** deleteSource removes a source and everything derived from it. */
export async function deleteSource(sourceId: string): Promise<void> {
  const res = await fetch(`/api/sources/${encodeURIComponent(sourceId)}`, { method: "DELETE" });
  if (!res.ok) throw new Error(`delete source failed: ${res.status}`);
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

// UploadFile pairs a file with the path it should keep relative to the source
// root (e.g. "guide/intro.md"). The backend preserves this structure.
export interface UploadFile {
  file: File;
  path: string;
}

// MAX_UPLOAD_BYTES mirrors the API's SERVER_MAX_UPLOAD_BYTES default (100 MiB).
// It is a client-side pre-check only; the server enforces the real limit, so a
// mismatch just means a batch that slips past here is rejected server-side.
export const MAX_UPLOAD_BYTES = 100 * 1024 * 1024;

export async function uploadSource(
  workspaceId: string,
  name: string,
  files: UploadFile[],
): Promise<UploadResult> {
  const form = new FormData();
  form.append("workspace_id", workspaceId);
  form.append("name", name);
  // The third argument sets each part's filename to the relative path, which the
  // backend reads (and sanitizes) to preserve directory structure.
  for (const { file, path } of files) form.append("files", file, path);

  const res = await fetch("/api/sources", { method: "POST", body: form });
  if (!res.ok) {
    const detail = (await res.text().catch(() => "")).trim();
    throw new Error(detail || `upload failed: ${res.status}`);
  }
  return res.json();
}

// UploadGroup is one source-to-be: a name and the files that belong to it.
export interface UploadGroup {
  name: string;
  files: UploadFile[];
}

/**
 * filesToUploadFiles maps a flat file selection to UploadFile[] keyed by each
 * file's own name (no directory structure). Used by the plain file picker.
 */
export function filesToUploadFiles(fileList: FileList): UploadFile[] {
  return Array.from(fileList).map((file) => ({ file, path: file.name }));
}

/**
 * groupFilesByFolder splits a directory selection into one UploadGroup per
 * source. Files directly under the selected folder form a single source named
 * after that folder; each top-level subfolder becomes its own source named
 * after the subfolder, with paths kept relative to it.
 */
export function groupFilesByFolder(fileList: FileList): UploadGroup[] {
  const groups = new Map<string, UploadFile[]>();

  for (const file of Array.from(fileList)) {
    const rel = file.webkitRelativePath || file.name;
    const segments = rel.split("/");

    let name: string;
    let path: string;
    if (segments.length <= 2) {
      // "folder/file" — directly under the selected folder.
      name = segments[0];
      path = segments[segments.length - 1];
    } else {
      // "folder/sub/.../file" — each top-level subfolder is its own source.
      name = segments[1];
      path = segments.slice(2).join("/");
    }

    const existing = groups.get(name) ?? [];
    existing.push({ file, path });
    groups.set(name, existing);
  }

  return Array.from(groups.entries()).map(([name, files]) => ({ name, files }));
}

export function totalBytes(files: UploadFile[]): number {
  return files.reduce((sum, { file }) => sum + file.size, 0);
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
