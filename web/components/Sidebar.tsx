"use client";

import { useQueryClient } from "@tanstack/react-query";
import { useId, useState } from "react";
import { File, Loader2, UploadCloud } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  filesToUploadFiles,
  groupFilesByFolder,
  totalBytes,
  watchSourceStatus,
  MAX_UPLOAD_BYTES,
} from "@/lib/api/sources";
import { sourcesQueryKey, useUploadSourceMutation } from "@/hooks/use-sources";
import { formatBytes } from "@/lib/utils";

type UploadState = "uploading" | "indexing" | "indexed" | "error";

interface UploadItem {
  name: string;
  fileCount: number;
  state: UploadState;
  message?: string;
}

// Beyond this many selected files, the pre-upload preview truncates and shows a count instead.
const MAX_FILE_PREVIEW = 6;

function mib(bytes: number): string {
  return `${(bytes / (1024 * 1024)).toFixed(0)} MiB`;
}

// isFolderSelection tells a folder pick (webkitdirectory) apart from a flat
// multi-file pick: every entry from a folder picker carries webkitRelativePath.
function isFolderSelection(list: FileList): boolean {
  return list.length > 0 && list[0].webkitRelativePath !== "";
}

function StatusBadge({ item }: { item: UploadItem }) {
  const inProgress = item.state === "uploading" || item.state === "indexing";
  return (
    <span className="flex shrink-0 items-center gap-1">
      {inProgress && <Loader2 className="size-3 animate-spin text-muted-foreground" />}
      <span className={`badge ${item.state === "error" ? "error" : item.state === "indexed" ? "indexed" : "indexing"}`}>
        {item.state}
      </span>
    </span>
  );
}

/**
 * Sidebar lets the user upload a new source into a workspace — either a
 * hand-picked set of files (one source) or a whole folder. A folder that
 * directly contains files becomes one source named after it; a folder of
 * subfolders becomes one source per top-level subfolder, uploaded as
 * separate batches. Which of the two happened is inferred from the
 * selection itself, so there's a single upload flow rather than a mode the
 * user has to pick upfront.
 *
 * Once a batch starts, `uploads` replaces the pre-upload preview in place
 * and tracks each item live through upload -> indexing -> indexed/error, so
 * the user isn't just staring at a static "Uploading…" button for a batch
 * that can take a while.
 */
export default function Sidebar({ workspaceId }: { workspaceId: string }) {
  const queryClient = useQueryClient();
  const uploadMutation = useUploadSourceMutation(workspaceId);
  const filesInputId = useId();
  const folderInputId = useId();
  const [name, setName] = useState("");
  const [files, setFiles] = useState<FileList | null>(null);
  const [busy, setBusy] = useState(false);
  const [uploads, setUploads] = useState<UploadItem[]>([]);

  function updateUpload(name: string, patch: Partial<UploadItem>) {
    setUploads((items) => items.map((it) => (it.name === name ? { ...it, ...patch } : it)));
  }

  // watch subscribes to a newly created source's indexing progress, updates
  // that item's live status, and invalidates the sources list once it's done.
  function watch(sourceId: string, name: string) {
    watchSourceStatus(sourceId, {
      onDone: () => {
        void queryClient.invalidateQueries({ queryKey: sourcesQueryKey(workspaceId) });
        updateUpload(name, { state: "indexed" });
      },
      onError: () => {
        void queryClient.invalidateQueries({ queryKey: sourcesQueryKey(workspaceId) });
        updateUpload(name, { state: "error", message: "indexing failed" });
      },
    });
  }

  function onSelect(list: FileList | null) {
    setFiles(list);
    setUploads([]);
  }

  async function onUploadFiles() {
    if (!files || files.length === 0 || !name) return;
    setBusy(true);
    setUploads([{ name, fileCount: files.length, state: "uploading" }]);
    try {
      const { source } = await uploadMutation.mutateAsync({
        name,
        files: filesToUploadFiles(files),
      });
      setFiles(null);
      updateUpload(name, { state: "indexing" });
      watch(source.ID, name);
    } catch (err) {
      updateUpload(name, { state: "error", message: err instanceof Error ? err.message : "upload failed" });
    } finally {
      setBusy(false);
    }
  }

  async function onUploadFolder() {
    if (!files || files.length === 0) return;
    setBusy(true);

    const groups = groupFilesByFolder(files);
    setUploads(groups.map((g) => ({ name: g.name, fileCount: g.files.length, state: "uploading" })));

    for (const group of groups) {
      const size = totalBytes(group.files);
      if (size > MAX_UPLOAD_BYTES) {
        updateUpload(group.name, {
          state: "error",
          message: `${mib(size)} exceeds the ${mib(MAX_UPLOAD_BYTES)} upload limit`,
        });
        continue;
      }
      try {
        const { source } = await uploadMutation.mutateAsync({
          name: group.name,
          files: group.files,
        });
        updateUpload(group.name, { state: "indexing" });
        watch(source.ID, group.name);
      } catch (err) {
        updateUpload(group.name, {
          state: "error",
          message: err instanceof Error ? err.message : "upload failed",
        });
      }
    }

    setFiles(null);
    setBusy(false);
  }

  function onSubmit(e: React.FormEvent) {
    e.preventDefault();
    void (isFolder ? onUploadFolder() : onUploadFiles());
  }

  const fileList = files ? Array.from(files) : [];
  const isFolder = files ? isFolderSelection(files) : false;
  const folderGroups = isFolder && files ? groupFilesByFolder(files) : [];
  const canSubmit = !busy && fileList.length > 0 && (isFolder || !!name);

  return (
    <div>
      <form className="flex flex-col gap-4" onSubmit={onSubmit}>
        <div className="flex flex-col gap-1.5">
          <Label htmlFor={filesInputId}>Files or folder</Label>
          <div className="flex flex-col items-center gap-1 rounded-lg border border-dashed border-border bg-muted/40 px-3 py-5 text-center transition-colors has-[label:hover]:border-primary has-[label:hover]:bg-accent">
            <UploadCloud className="size-5 text-muted-foreground" />
            <span className="text-xs text-muted-foreground">
              {fileList.length > 0
                ? `${fileList.length} file${fileList.length === 1 ? "" : "s"} selected`
                : "Pick individual files, or a whole folder to index"}
            </span>
            <span className="flex items-center gap-1.5 text-sm font-medium">
              <label htmlFor={filesInputId} className="cursor-pointer text-primary hover:underline">
                Choose files
              </label>
              <span className="text-muted-foreground">or</span>
              <label htmlFor={folderInputId} className="cursor-pointer text-primary hover:underline">
                choose a folder
              </label>
            </span>
          </div>
          <input
            id={filesInputId}
            type="file"
            multiple
            className="sr-only"
            onChange={(e) => onSelect(e.target.files)}
            key={busy ? "files-busy" : "files-idle"}
          />
          <input
            id={folderInputId}
            type="file"
            multiple
            className="sr-only"
            // webkitdirectory isn't in the standard input typings, so set it on
            // the element directly. It turns the picker into a folder picker.
            ref={(node) => {
              if (node) node.setAttribute("webkitdirectory", "");
            }}
            onChange={(e) => onSelect(e.target.files)}
            key={busy ? "folder-busy" : "folder-idle"}
          />
        </div>

        {!isFolder && uploads.length === 0 && fileList.length > 0 && (
          <div className="flex flex-col gap-1.5">
            <Label htmlFor="source-name">Name</Label>
            <Input
              id="source-name"
              type="text"
              placeholder="e.g. Product docs"
              value={name}
              onChange={(e) => setName(e.target.value)}
            />
          </div>
        )}

        {uploads.length > 0 ? (
          <ul className="flex flex-col gap-1">
            {uploads.map((item) => (
              <li
                key={item.name}
                className="rounded-md border border-border bg-muted px-2 py-1 text-xs"
              >
                <div className="flex items-center gap-1.5">
                  <File className="size-3.5 shrink-0 text-muted-foreground" />
                  <span className="truncate">{item.name}</span>
                  <span className="ml-auto shrink-0 text-muted-foreground">
                    {item.fileCount} file{item.fileCount === 1 ? "" : "s"}
                  </span>
                  <StatusBadge item={item} />
                </div>
                {item.state === "error" && item.message && (
                  <div className="mt-1 text-destructive">{item.message}</div>
                )}
              </li>
            ))}
          </ul>
        ) : !isFolder && fileList.length > 0 ? (
          <ul className="flex flex-col gap-1">
            {fileList.slice(0, MAX_FILE_PREVIEW).map((f) => (
              <li
                key={f.name}
                className="flex items-center gap-1.5 rounded-md border border-border bg-muted px-2 py-1 text-xs"
              >
                <File className="size-3.5 shrink-0 text-muted-foreground" />
                <span className="truncate">{f.name}</span>
                <span className="ml-auto shrink-0 text-muted-foreground">{formatBytes(f.size)}</span>
              </li>
            ))}
            {fileList.length > MAX_FILE_PREVIEW && (
              <li className="hint">+{fileList.length - MAX_FILE_PREVIEW} more</li>
            )}
          </ul>
        ) : (
          isFolder &&
          folderGroups.length > 0 && (
            <ul className="flex flex-col gap-1">
              {folderGroups.map((g) => (
                <li
                  key={g.name}
                  className="flex items-center gap-1.5 rounded-md border border-border bg-muted px-2 py-1 text-xs"
                >
                  <File className="size-3.5 shrink-0 text-muted-foreground" />
                  <span className="truncate">{g.name}</span>
                  <span className="ml-auto shrink-0 text-muted-foreground">
                    {g.files.length} file{g.files.length === 1 ? "" : "s"}
                  </span>
                </li>
              ))}
            </ul>
          )
        )}

        {isFolder && uploads.length === 0 && (
          <div className="hint">
            Each top-level subfolder becomes its own source; files directly in the
            folder become one source named after it.
          </div>
        )}

        <Button type="submit" disabled={!canSubmit}>
          {busy ? "Uploading…" : isFolder ? "Upload folder(s)" : "Upload & index"}
        </Button>
      </form>
    </div>
  );
}
