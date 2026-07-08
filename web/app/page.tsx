"use client";

import { useCallback, useEffect, useState } from "react";
import Chat from "@/components/Chat";
import Sidebar from "@/components/Sidebar";
import SignIn from "@/components/SignIn";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { createWorkspace, getMe, listWorkspaces, logout } from "@/lib/api";
import type { Me, Workspace } from "@/lib/types";

export default function Home() {
  // undefined = loading, null = unauthenticated, Me = signed in.
  const [me, setMe] = useState<Me | null | undefined>(undefined);
  const [workspaces, setWorkspaces] = useState<Workspace[]>([]);
  const [activeId, setActiveId] = useState<string>("");

  useEffect(() => {
    getMe()
      .then(setMe)
      .catch(() => setMe(null));
  }, []);

  const loadWorkspaces = useCallback(async () => {
    const list = await listWorkspaces();
    setWorkspaces(list);
    setActiveId((prev) => prev || list[0]?.ID || "");
  }, []);

  useEffect(() => {
    // eslint-disable-next-line react-hooks/set-state-in-effect -- loads workspaces once `me` resolves
    if (me) void loadWorkspaces();
  }, [me, loadWorkspaces]);

  async function onCreateWorkspace() {
    const name = window.prompt("New workspace name");
    if (!name) return;
    const ws = await createWorkspace(name);
    await loadWorkspaces();
    setActiveId(ws.ID);
  }

  async function onLogout() {
    await logout();
    setMe(null);
    setWorkspaces([]);
    setActiveId("");
  }

  if (me === undefined) return <div className="center hint">Loading…</div>;
  if (me === null) return <SignIn />;

  return (
    <div className="app">
      <div className="topbar">
        <span className="brand">NeuralVault</span>
        <Select
          value={activeId}
          onValueChange={setActiveId}
          disabled={workspaces.length === 0}
        >
          <SelectTrigger>
            <SelectValue placeholder="No workspaces" />
          </SelectTrigger>
          <SelectContent>
            {workspaces.map((w) => (
              <SelectItem key={w.ID} value={w.ID}>
                {w.Name}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        <Button variant="secondary" onClick={onCreateWorkspace}>
          + New
        </Button>
        <div className="spacer" />
        <span className="email">{me.email}</span>
        <Button variant="secondary" onClick={onLogout}>
          Sign out
        </Button>
      </div>

      {activeId ? (
        <div className="layout">
          <Sidebar workspaceId={activeId} />
          <Chat workspaceId={activeId} />
        </div>
      ) : (
        <div className="center">
          <Card className="items-center p-6 text-center">
            <p className="hint">Create a workspace to get started.</p>
            <Button onClick={onCreateWorkspace}>Create workspace</Button>
          </Card>
        </div>
      )}
    </div>
  );
}
