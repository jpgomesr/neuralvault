"use client";

import { useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { Files } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import SourceFilesDialog from "@/components/SourceFilesDialog";
import {
  filesToUploadFiles,
  groupFilesByFolder,
  totalBytes,
  watchSourceStatus,
  MAX_UPLOAD_BYTES,
} from "@/lib/api/sources";
import { sourcesQueryKey, useSources, useUploadSourceMutation } from "@/hooks/use-sources";

type UploadMode = "files" | "folder";

interface BatchResult {
  name: string;
  ok: boolean;
  message?: string;
}

function mib(bytes: number): string {
  return `${(bytes / (1024 * 1024)).toFixed(0)} MiB`;
}

/**
 * Sidebar lists a workspace's sources and lets the user upload new ones — either
 * a hand-picked set of files (one source) or a folder. A folder that directly
 * contains files becomes one source named after it; a folder of subfolders
 * becomes one source per top-level subfolder, uploaded as separate batches.
 */
export default function Sidebar({ workspaceId }: { workspaceId: string }) {
  const queryClient = useQueryClient();
  const { data: sources = [], error: sourcesError } = useSources(workspaceId);
  const uploadMutation = useUploadSourceMutation(workspaceId);
  const [mode, setMode] = useState<UploadMode>("files");
  const [name, setName] = useState("");
  const [files, setFiles] = useState<FileList | null>(null);
  const [busy, setBusy] = useState(false);
  const [formError, setFormError] = useState<string | null>(null);
  const [batchResults, setBatchResults] = useState<BatchResult[]>([]);
  const [liveStatus, setLiveStatus] = useState<Record<string, string>>({});
  const [preview, setPreview] = useState<{ id: string; name: string } | null>(null);

  // watch subscribes to a newly created source's indexing progress.
  function watch(sourceId: string) {
    setLiveStatus((s) => ({ ...s, [sourceId]: "indexing" }));
    watchSourceStatus(sourceId, {
      onProgress: () => setLiveStatus((s) => ({ ...s, [sourceId]: "indexing" })),
      onDone: () => {
        setLiveStatus((s) => ({ ...s, [sourceId]: "indexed" }));
        void queryClient.invalidateQueries({ queryKey: sourcesQueryKey(workspaceId) });
      },
      onError: () => setLiveStatus((s) => ({ ...s, [sourceId]: "error" })),
    });
  }

  function switchMode(next: UploadMode) {
    setMode(next);
    setFiles(null);
    setFormError(null);
    setBatchResults([]);
  }

  async function onUploadFiles() {
    if (!files || files.length === 0 || !name) return;
    setBusy(true);
    setFormError(null);
    try {
      const { source } = await uploadMutation.mutateAsync({
        name,
        files: filesToUploadFiles(files),
      });
      setName("");
      setFiles(null);
      watch(source.ID);
    } catch (err) {
      setFormError(err instanceof Error ? err.message : "upload failed");
    } finally {
      setBusy(false);
    }
  }

  async function onUploadFolder() {
    if (!files || files.length === 0) return;
    setBusy(true);
    setFormError(null);
    setBatchResults([]);

    const groups = groupFilesByFolder(files);
    const results: BatchResult[] = [];
    for (const group of groups) {
      const size = totalBytes(group.files);
      if (size > MAX_UPLOAD_BYTES) {
        results.push({
          name: group.name,
          ok: false,
          message: `${mib(size)} exceeds the ${mib(MAX_UPLOAD_BYTES)} upload limit`,
        });
        continue;
      }
      try {
        const { source } = await uploadMutation.mutateAsync({
          name: group.name,
          files: group.files,
        });
        watch(source.ID);
        results.push({ name: group.name, ok: true });
      } catch (err) {
        results.push({
          name: group.name,
          ok: false,
          message: err instanceof Error ? err.message : "upload failed",
        });
      }
    }

    setBatchResults(results);
    setFiles(null);
    setBusy(false);
  }

  function onSubmit(e: React.FormEvent) {
    e.preventDefault();
    void (mode === "folder" ? onUploadFolder() : onUploadFiles());
  }

  const error =
    formError ||
    (sourcesError instanceof Error && sourcesError.message) ||
    null;

  const canSubmit = !busy && !!files?.length && (mode === "folder" || !!name);

  return (
    <aside className="sidebar">
      <h2>Add a source</h2>

      <div className="mode-toggle">
        <Button
          type="button"
          size="sm"
          variant={mode === "files" ? "default" : "secondary"}
          onClick={() => switchMode("files")}
        >
          Files
        </Button>
        <Button
          type="button"
          size="sm"
          variant={mode === "folder" ? "default" : "secondary"}
          onClick={() => switchMode("folder")}
        >
          Folder
        </Button>
      </div>

      <form className="uploader" onSubmit={onSubmit}>
        {mode === "files" && (
          <Input
            type="text"
            placeholder="Source name"
            value={name}
            onChange={(e) => setName(e.target.value)}
          />
        )}

        {mode === "files" ? (
          <input
            type="file"
            multiple
            onChange={(e) => setFiles(e.target.files)}
            key={busy ? "files-busy" : "files-idle"}
          />
        ) : (
          <input
            type="file"
            multiple
            // webkitdirectory isn't in the standard input typings, so set it on
            // the element directly. It turns the picker into a folder picker.
            ref={(node) => {
              if (node) node.setAttribute("webkitdirectory", "");
            }}
            onChange={(e) => setFiles(e.target.files)}
            key={busy ? "folder-busy" : "folder-idle"}
          />
        )}

        {mode === "folder" && (
          <div className="hint">
            Each top-level subfolder becomes its own source; files directly in the
            folder become one source named after it.
          </div>
        )}

        <Button type="submit" disabled={!canSubmit}>
          {busy ? "Uploading…" : mode === "folder" ? "Upload folder(s)" : "Upload & index"}
        </Button>

        {error && <div className="error">{error}</div>}

        {batchResults.length > 0 && (
          <ul className="batch-results">
            {batchResults.map((r) => (
              <li key={r.name} className={r.ok ? "hint" : "error"}>
                {r.ok ? "✓" : "✗"} {r.name}
                {r.message ? ` — ${r.message}` : ""}
              </li>
            ))}
          </ul>
        )}
      </form>

      <h2>Sources</h2>
      {sources.length === 0 && <div className="hint">No sources yet.</div>}
      {sources.map((s) => {
        const status = liveStatus[s.ID] ?? s.Status;
        return (
          <div className="source" key={s.ID}>
            <span>{s.Name}</span>
            <span className="flex items-center gap-1.5">
              <span className={`badge ${status}`}>{status}</span>
              <Button
                type="button"
                variant="ghost"
                size="icon-xs"
                title="View files"
                aria-label={`View files of ${s.Name}`}
                onClick={() => setPreview({ id: s.ID, name: s.Name })}
              >
                <Files />
              </Button>
            </span>
          </div>
        );
      })}

      {preview && (
        <SourceFilesDialog
          sourceId={preview.id}
          sourceName={preview.name}
          open={preview !== null}
          onOpenChange={(o) => !o && setPreview(null)}
        />
      )}
    </aside>
  );
}
