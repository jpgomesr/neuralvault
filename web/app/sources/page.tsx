"use client";

import Link from "next/link";
import { useState } from "react";
import { Files, Search, Trash2 } from "lucide-react";
import SignIn from "@/components/SignIn";
import SourceFilesDialog from "@/components/SourceFilesDialog";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
  AlertDialogTrigger,
} from "@/components/ui/alert-dialog";
import WorkspaceSwitcher from "@/components/WorkspaceSwitcher";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { useLogoutMutation, useMe } from "@/hooks/use-me";
import { useDeleteSourceMutation, useSources } from "@/hooks/use-sources";
import { useActiveWorkspace } from "@/lib/workspace-context";
import type { Source } from "@/lib/types";

// Sources are paginated client-side over the full list the API returns
// today; see the tracking issue for moving this to real server-side paging
// (limit/offset + total count) once workspaces can have very large lists.
const PAGE_SIZE = 10;

export default function SourcesPage() {
  const { me } = useMe();
  const { activeId, setActiveId, isLoading: workspacesLoading } = useActiveWorkspace();
  const logoutMutation = useLogoutMutation();
  const { data: sources = [], isLoading, error } = useSources(activeId);
  const deleteMutation = useDeleteSourceMutation(activeId);
  const [preview, setPreview] = useState<{ id: string; name: string } | null>(null);
  const [deleteError, setDeleteError] = useState<string | null>(null);
  const [query, setQuery] = useState("");
  const [page, setPage] = useState(1);

  function onLogout() {
    logoutMutation.mutate(undefined, { onSuccess: () => setActiveId("") });
  }

  function onSearch(value: string) {
    setQuery(value);
    setPage(1);
  }

  async function onDelete(source: Source) {
    setDeleteError(null);
    try {
      await deleteMutation.mutateAsync(source.ID);
    } catch (err) {
      setDeleteError(err instanceof Error ? err.message : "delete failed");
    }
  }

  if (me === undefined) return <div className="center hint">Loading…</div>;
  if (me === null) return <SignIn />;
  if (workspacesLoading) return <div className="center hint">Loading…</div>;

  const filtered = sources.filter((s) =>
    s.Name.toLowerCase().includes(query.trim().toLowerCase()),
  );
  const totalPages = Math.max(1, Math.ceil(filtered.length / PAGE_SIZE));
  // Clamps naturally back into range after a delete or workspace switch
  // shrinks the list, without a separate effect to reset `page`.
  const activePage = Math.min(page, totalPages);
  const pageItems = filtered.slice((activePage - 1) * PAGE_SIZE, activePage * PAGE_SIZE);

  return (
    <div className="app">
      <div className="topbar">
        <Link href="/" className="brand">
          NeuralVault
        </Link>
        <WorkspaceSwitcher />
        <Button variant="ghost" asChild>
          <Link href="/">← Chat</Link>
        </Button>
        <div className="spacer" />
        <span className="email">{me.email}</span>
        <Button variant="secondary" onClick={onLogout}>
          Sign out
        </Button>
      </div>

      <div className="overflow-y-auto p-6">
        <div className="mx-auto flex max-w-4xl flex-col gap-4">
          <div className="flex items-center justify-between gap-4">
            <div>
              <h1 className="text-lg font-semibold">Sources</h1>
              {sources.length > 0 && (
                <p className="hint">
                  {sources.length} source{sources.length === 1 ? "" : "s"}
                </p>
              )}
            </div>
            {sources.length > 0 && (
              <div className="relative w-64">
                <Search className="pointer-events-none absolute top-1/2 left-2.5 size-3.5 -translate-y-1/2 text-muted-foreground" />
                <Input
                  placeholder="Search sources…"
                  value={query}
                  onChange={(e) => onSearch(e.target.value)}
                  className="pl-8"
                />
              </div>
            )}
          </div>

          {!activeId && <div className="hint">Create a workspace to see its sources.</div>}
          {isLoading && <div className="hint">Loading…</div>}
          {error && <div className="error">Failed to load sources.</div>}
          {deleteError && <div className="error">{deleteError}</div>}

          {activeId && !isLoading && sources.length === 0 && (
            <div className="hint">No sources yet.</div>
          )}
          {sources.length > 0 && filtered.length === 0 && (
            <div className="hint">No sources match &quot;{query}&quot;.</div>
          )}

          {pageItems.length > 0 && (
            <div className="overflow-hidden rounded-lg border border-border">
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>Name</TableHead>
                    <TableHead>Status</TableHead>
                    <TableHead className="text-right">Actions</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {pageItems.map((s) => (
                    <TableRow key={s.ID}>
                      <TableCell>{s.Name}</TableCell>
                      <TableCell>
                        <span className={`badge ${s.Status}`}>{s.Status}</span>
                      </TableCell>
                      <TableCell className="text-right">
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
                        <AlertDialog>
                          <AlertDialogTrigger asChild>
                            <Button
                              type="button"
                              variant="ghost"
                              size="icon-xs"
                              title="Delete source"
                              aria-label={`Delete ${s.Name}`}
                            >
                              <Trash2 />
                            </Button>
                          </AlertDialogTrigger>
                          <AlertDialogContent>
                            <AlertDialogHeader>
                              <AlertDialogTitle>Delete &quot;{s.Name}&quot;?</AlertDialogTitle>
                              <AlertDialogDescription>
                                This removes the source and everything indexed from it. This
                                cannot be undone.
                              </AlertDialogDescription>
                            </AlertDialogHeader>
                            <AlertDialogFooter>
                              <AlertDialogCancel>Cancel</AlertDialogCancel>
                              <AlertDialogAction
                                variant="destructive"
                                onClick={() => onDelete(s)}
                                disabled={deleteMutation.isPending}
                              >
                                {deleteMutation.isPending ? "Deleting…" : "Delete"}
                              </AlertDialogAction>
                            </AlertDialogFooter>
                          </AlertDialogContent>
                        </AlertDialog>
                      </TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            </div>
          )}

          {totalPages > 1 && (
            <div className="flex items-center justify-between text-sm">
              <span className="text-muted-foreground">
                Page {activePage} of {totalPages}
              </span>
              <div className="flex gap-2">
                <Button
                  type="button"
                  variant="secondary"
                  size="sm"
                  disabled={activePage <= 1}
                  onClick={() => setPage(activePage - 1)}
                >
                  Previous
                </Button>
                <Button
                  type="button"
                  variant="secondary"
                  size="sm"
                  disabled={activePage >= totalPages}
                  onClick={() => setPage(activePage + 1)}
                >
                  Next
                </Button>
              </div>
            </div>
          )}
        </div>
      </div>

      {preview && (
        <SourceFilesDialog
          sourceId={preview.id}
          sourceName={preview.name}
          open={preview !== null}
          onOpenChange={(o) => !o && setPreview(null)}
        />
      )}
    </div>
  );
}
