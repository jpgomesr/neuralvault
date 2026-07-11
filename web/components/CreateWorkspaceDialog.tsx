"use client";

import { useState } from "react";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { useCreateWorkspaceMutation } from "@/hooks/use-workspaces";
import type { Workspace } from "@/lib/types";

/** CreateWorkspaceDialog is a controlled dialog for creating a new workspace. */
export default function CreateWorkspaceDialog({
  open,
  onOpenChange,
  onCreated,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onCreated?: (workspace: Workspace) => void;
}) {
  const createWorkspaceMutation = useCreateWorkspaceMutation();
  const [name, setName] = useState("");
  const [error, setError] = useState<string | null>(null);

  function onChange(next: boolean) {
    if (next) {
      setName("");
      setError(null);
    }
    onOpenChange(next);
  }

  async function onSubmit(e: React.FormEvent) {
    e.preventDefault();
    const trimmed = name.trim();
    if (!trimmed) return;
    setError(null);
    try {
      const workspace = await createWorkspaceMutation.mutateAsync(trimmed);
      onOpenChange(false);
      onCreated?.(workspace);
    } catch (err) {
      setError(err instanceof Error ? err.message : "failed to create workspace");
    }
  }

  return (
    <Dialog open={open} onOpenChange={onChange}>
      <DialogContent className="max-w-sm">
        <DialogHeader>
          <DialogTitle>Create workspace</DialogTitle>
          <DialogDescription>
            Sources and conversations stay scoped to a workspace, so you can switch
            between them anytime.
          </DialogDescription>
        </DialogHeader>
        <form className="flex flex-col gap-3" onSubmit={onSubmit}>
          <div className="flex flex-col gap-1.5">
            <Label htmlFor="workspace-name">Name</Label>
            <Input
              id="workspace-name"
              autoFocus
              placeholder="e.g. Acme Corp"
              value={name}
              onChange={(e) => setName(e.target.value)}
            />
          </div>
          {error && <div className="error">{error}</div>}
          <div className="flex justify-end gap-2">
            <Button type="button" variant="secondary" onClick={() => onOpenChange(false)}>
              Cancel
            </Button>
            <Button type="submit" disabled={!name.trim() || createWorkspaceMutation.isPending}>
              {createWorkspaceMutation.isPending ? "Creating…" : "Create"}
            </Button>
          </div>
        </form>
      </DialogContent>
    </Dialog>
  );
}
