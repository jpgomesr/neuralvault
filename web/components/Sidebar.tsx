"use client";

import { useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { watchSourceStatus } from "@/lib/api/sources";
import { sourcesQueryKey, useSources, useUploadSourceMutation } from "@/hooks/use-sources";

/**
 * Sidebar lists a workspace's sources and lets the user upload a new one,
 * showing live indexing status streamed from the API.
 */
export default function Sidebar({ workspaceId }: { workspaceId: string }) {
  const queryClient = useQueryClient();
  const { data: sources = [], error: sourcesError } = useSources(workspaceId);
  const uploadMutation = useUploadSourceMutation(workspaceId);
  const [name, setName] = useState("");
  const [files, setFiles] = useState<FileList | null>(null);
  const [liveStatus, setLiveStatus] = useState<Record<string, string>>({});

  async function onUpload(e: React.FormEvent) {
    e.preventDefault();
    if (!files || files.length === 0 || !name) return;
    try {
      const { source } = await uploadMutation.mutateAsync({ name, files });
      setName("");
      setFiles(null);
      setLiveStatus((s) => ({ ...s, [source.ID]: "indexing" }));
      watchSourceStatus(source.ID, {
        onProgress: () => setLiveStatus((s) => ({ ...s, [source.ID]: "indexing" })),
        onDone: () => {
          setLiveStatus((s) => ({ ...s, [source.ID]: "indexed" }));
          void queryClient.invalidateQueries({ queryKey: sourcesQueryKey(workspaceId) });
        },
        onError: () => setLiveStatus((s) => ({ ...s, [source.ID]: "error" })),
      });
    } catch {
      // surfaced below via uploadMutation.error
    }
  }

  const busy = uploadMutation.isPending;
  const error =
    (uploadMutation.error instanceof Error && uploadMutation.error.message) ||
    (sourcesError instanceof Error && sourcesError.message) ||
    null;

  return (
    <aside className="sidebar">
      <h2>Add a source</h2>
      <form className="uploader" onSubmit={onUpload}>
        <Input
          type="text"
          placeholder="Source name"
          value={name}
          onChange={(e) => setName(e.target.value)}
        />
        <input
          type="file"
          multiple
          onChange={(e) => setFiles(e.target.files)}
          key={busy ? "busy" : "idle"}
        />
        <Button type="submit" disabled={busy || !name || !files?.length}>
          {busy ? "Uploading…" : "Upload & index"}
        </Button>
        {error && <div className="error">{error}</div>}
      </form>

      <h2>Sources</h2>
      {sources.length === 0 && <div className="hint">No sources yet.</div>}
      {sources.map((s) => {
        const status = liveStatus[s.ID] ?? s.Status;
        return (
          <div className="source" key={s.ID}>
            <span>{s.Name}</span>
            <span className={`badge ${status}`}>{status}</span>
          </div>
        );
      })}
    </aside>
  );
}
