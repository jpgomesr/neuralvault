"use client";

import { useCallback, useEffect, useState } from "react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { listSources, uploadSource, watchSourceStatus } from "@/lib/api";
import type { Source } from "@/lib/types";

/**
 * Sidebar lists a workspace's sources and lets the user upload a new one,
 * showing live indexing status streamed from the API.
 */
export default function Sidebar({ workspaceId }: { workspaceId: string }) {
  const [sources, setSources] = useState<Source[]>([]);
  const [name, setName] = useState("");
  const [files, setFiles] = useState<FileList | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [liveStatus, setLiveStatus] = useState<Record<string, string>>({});

  const refresh = useCallback(async () => {
    try {
      setSources(await listSources(workspaceId));
    } catch (e) {
      setError(e instanceof Error ? e.message : "failed to load sources");
    }
  }, [workspaceId]);

  useEffect(() => {
    // eslint-disable-next-line react-hooks/set-state-in-effect -- resets local state before refreshing sources
    setSources([]);
    setLiveStatus({});
    void refresh();
  }, [refresh]);

  async function onUpload(e: React.FormEvent) {
    e.preventDefault();
    if (!files || files.length === 0 || !name) return;
    setBusy(true);
    setError(null);
    try {
      const { source } = await uploadSource(workspaceId, name, files);
      setName("");
      setFiles(null);
      setLiveStatus((s) => ({ ...s, [source.ID]: "indexing" }));
      await refresh();
      watchSourceStatus(source.ID, {
        onProgress: () => setLiveStatus((s) => ({ ...s, [source.ID]: "indexing" })),
        onDone: () => {
          setLiveStatus((s) => ({ ...s, [source.ID]: "indexed" }));
          void refresh();
        },
        onError: () => setLiveStatus((s) => ({ ...s, [source.ID]: "error" })),
      });
    } catch (e) {
      setError(e instanceof Error ? e.message : "upload failed");
    } finally {
      setBusy(false);
    }
  }

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
