"use client";

import { useState } from "react";
import { Check, ChevronDown, Plus, Settings } from "lucide-react";
import CreateWorkspaceDialog from "@/components/CreateWorkspaceDialog";
import ModelSettingsDialog from "@/components/ModelSettingsDialog";
import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { useActiveWorkspace } from "@/lib/workspace-context";

/**
 * WorkspaceSwitcher is the single place workspace selection and creation
 * live, so workspace-level actions (member management, settings, etc.) have
 * one component to grow into later instead of being spread across pages.
 */
export default function WorkspaceSwitcher() {
  const { workspaces, activeId, setActiveId } = useActiveWorkspace();
  const [createOpen, setCreateOpen] = useState(false);
  const [settingsOpen, setSettingsOpen] = useState(false);
  const active = workspaces.find((w) => w.ID === activeId);

  return (
    <>
      <DropdownMenu>
        <DropdownMenuTrigger asChild>
          <Button variant="secondary" className="gap-1.5">
            <span className="max-w-40 truncate">{active?.Name ?? "No workspaces"}</span>
            <ChevronDown className="size-3.5 text-muted-foreground" />
          </Button>
        </DropdownMenuTrigger>
        <DropdownMenuContent align="start" className="w-56">
          {workspaces.map((w) => (
            <DropdownMenuItem key={w.ID} onSelect={() => setActiveId(w.ID)}>
              <span className="flex-1 truncate">{w.Name}</span>
              {w.ID === activeId && <Check className="size-3.5" />}
            </DropdownMenuItem>
          ))}
          {workspaces.length > 0 && <DropdownMenuSeparator />}
          <DropdownMenuItem disabled={!activeId} onSelect={() => setSettingsOpen(true)}>
            <Settings className="size-3.5" />
            <span className="flex-1">Model settings</span>
          </DropdownMenuItem>
          <DropdownMenuItem onSelect={() => setCreateOpen(true)}>
            <Plus className="size-3.5" />
            Create workspace
          </DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>

      <CreateWorkspaceDialog
        open={createOpen}
        onOpenChange={setCreateOpen}
        onCreated={(ws) => setActiveId(ws.ID)}
      />

      {activeId && (
        <ModelSettingsDialog
          workspaceId={activeId}
          open={settingsOpen}
          onOpenChange={setSettingsOpen}
        />
      )}
    </>
  );
}
