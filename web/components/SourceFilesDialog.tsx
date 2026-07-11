"use client";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import Markdown from "@/components/Markdown";
import { useSourceFiles } from "@/hooks/use-sources";
import { fetchSourceFileText, sourceFileContentUrl } from "@/lib/api/sources";
import type { SourceFile } from "@/lib/types";
import { cn, formatBytes } from "@/lib/utils";

type PreviewKind = "markdown" | "text" | "image" | "pdf" | "download";

const TEXT_EXTS = new Set([
  "txt", "text", "log", "json", "csv", "tsv", "yaml", "yml", "xml", "html",
  "htm", "css", "js", "jsx", "ts", "tsx", "py", "go", "rs", "java", "c", "cpp",
  "h", "sh", "sql", "toml", "ini", "env", "gitignore",
]);
const IMAGE_EXTS = new Set(["png", "jpg", "jpeg", "gif", "webp", "svg", "bmp", "ico", "avif"]);

function ext(name: string): string {
  const dot = name.lastIndexOf(".");
  return dot === -1 ? "" : name.slice(dot + 1).toLowerCase();
}

function previewKind(file: SourceFile): PreviewKind {
  const e = ext(file.name);
  const type = file.content_type ?? "";
  if (e === "md" || e === "markdown") return "markdown";
  if (e === "pdf" || type === "application/pdf") return "pdf";
  if (IMAGE_EXTS.has(e) || type.startsWith("image/")) return "image";
  if (TEXT_EXTS.has(e) || type.startsWith("text/")) return "text";
  return "download";
}

function FilePreview({ sourceId, file }: { sourceId: string; file: SourceFile }) {
  const kind = previewKind(file);
  const url = sourceFileContentUrl(sourceId, file.name);
  const isText = kind === "markdown" || kind === "text";

  const { data: text, isLoading, error } = useQuery({
    queryKey: ["sourceFileText", sourceId, file.name],
    queryFn: () => fetchSourceFileText(sourceId, file.name),
    enabled: isText,
  });

  if (kind === "image") {
    // eslint-disable-next-line @next/next/no-img-element -- previewing arbitrary user files, not an optimizable asset
    return <img src={url} alt={file.name} className="max-w-full" />;
  }
  if (kind === "pdf") {
    return <iframe src={url} title={file.name} className="h-[60vh] w-full rounded-md border border-border" />;
  }
  if (!isText) {
    return (
      <a href={url} download className="btn secondary">
        Download {file.name}
      </a>
    );
  }
  if (isLoading) return <div className="hint">Loading…</div>;
  if (error) return <div className="error">Failed to load file.</div>;
  if (kind === "markdown") return <Markdown>{text ?? ""}</Markdown>;
  return <pre className="markdown-pre">{text}</pre>;
}

/**
 * SourceFilesDialog previews a source's original files. The file list is fetched
 * only while the dialog is open; selecting a file renders it inline (markdown,
 * text, image, PDF) or offers a download link for other types.
 */
export default function SourceFilesDialog({
  sourceId,
  sourceName,
  initialFile,
  open,
  onOpenChange,
}: {
  sourceId: string;
  sourceName: string;
  /** Pre-select a specific file (e.g. the one a chat citation pointed at). */
  initialFile?: string;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const { data: files = [], isLoading, error } = useSourceFiles(sourceId, open);
  const [selected, setSelected] = useState<string | null>(null);

  // Fall back to initialFile, then the first file, until the user picks one.
  // The dialog is remounted on each open (parent renders it conditionally),
  // so selection resets naturally without an effect.
  const activeName = selected ?? initialFile ?? files[0]?.name ?? null;
  const active = files.find((f) => f.name === activeName) ?? null;

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-4xl">
        <DialogHeader>
          <DialogTitle>{sourceName} — files</DialogTitle>
        </DialogHeader>

        {isLoading && <div className="hint">Loading files…</div>}
        {error && <div className="error">Failed to load files.</div>}
        {!isLoading && !error && files.length === 0 && (
          <div className="hint">No files stored for this source.</div>
        )}

        {files.length > 0 && (
          <div className="grid grid-cols-[220px_1fr] gap-4">
            <ul className="max-h-[65vh] overflow-y-auto border-r border-border pr-2 text-sm">
              {files.map((f) => (
                <li key={f.id}>
                  <button
                    type="button"
                    onClick={() => setSelected(f.name)}
                    className={cn(
                      "flex w-full flex-col items-start gap-0.5 rounded-md px-2 py-1.5 text-left hover:bg-accent",
                      f.name === activeName && "bg-accent"
                    )}
                  >
                    <span className="break-all">{f.name}</span>
                    <span className="text-xs text-muted-foreground">{formatBytes(f.size)}</span>
                  </button>
                </li>
              ))}
            </ul>
            <div className="max-h-[65vh] min-w-0 overflow-auto pr-3">
              {active ? (
                <FilePreview sourceId={sourceId} file={active} />
              ) : (
                <div className="hint">Select a file to preview.</div>
              )}
            </div>
          </div>
        )}
      </DialogContent>
    </Dialog>
  );
}
