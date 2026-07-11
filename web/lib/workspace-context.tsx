"use client";

import { createContext, useContext, useState, type ReactNode } from "react";
import { useMe } from "@/hooks/use-me";
import { useWorkspaces } from "@/hooks/use-workspaces";
import type { Workspace } from "./types";

interface WorkspaceContextValue {
  workspaces: Workspace[];
  /** True until the first workspace fetch settles — lets callers tell "no
   * workspaces yet" apart from "still loading", so a logged-in user with
   * workspaces doesn't flash an empty "create a workspace" state. */
  isLoading: boolean;
  activeId: string;
  setActiveId: (id: string) => void;
}

const WorkspaceContext = createContext<WorkspaceContextValue | null>(null);

/**
 * WorkspaceProvider owns the currently selected workspace so pages other than
 * the chat home page (e.g. /sources) can read and change it without redoing
 * the workspace list fetch or the "default to the first workspace" logic.
 */
export function WorkspaceProvider({ children }: { children: ReactNode }) {
  const { me } = useMe();
  const { data: workspaces = [], isPending } = useWorkspaces(!!me);
  const [rawActiveId, setActiveId] = useState<string>("");

  // Derived rather than synced via an effect: an effect keyed on `workspaces`
  // only re-runs when that reference changes, but TanStack Query's
  // structural sharing can hand back the *same* reference after a
  // logout/login round trip that returns identical data — so the "default to
  // the first workspace" logic would silently never re-fire and activeId
  // would stay stuck at "" from logout. Recomputing on every render sidesteps
  // that: it only falls back to the first workspace while nothing has been
  // explicitly chosen yet (rawActiveId is falsy), same as before.
  const activeId = rawActiveId || workspaces[0]?.ID || "";

  return (
    <WorkspaceContext.Provider
      value={{ workspaces, isLoading: !!me && isPending, activeId, setActiveId }}
    >
      {children}
    </WorkspaceContext.Provider>
  );
}

/** useActiveWorkspace reads the workspace list and current selection. */
export function useActiveWorkspace() {
  const ctx = useContext(WorkspaceContext);
  if (!ctx) throw new Error("useActiveWorkspace must be used within a WorkspaceProvider");
  return ctx;
}
