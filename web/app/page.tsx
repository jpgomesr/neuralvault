"use client";

import { useCallback, useEffect, useState } from "react";
import Chat from "@/components/Chat";
import Sidebar from "@/components/Sidebar";
import SignIn from "@/components/SignIn";
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
        <select value={activeId} onChange={(e) => setActiveId(e.target.value)}>
          {workspaces.length === 0 && <option value="">No workspaces</option>}
          {workspaces.map((w) => (
            <option key={w.ID} value={w.ID}>
              {w.Name}
            </option>
          ))}
        </select>
        <button className="btn secondary" onClick={onCreateWorkspace}>
          + New
        </button>
        <div className="spacer" />
        <span className="email">{me.email}</span>
        <button className="btn secondary" onClick={onLogout}>
          Sign out
        </button>
      </div>

      {activeId ? (
        <div className="layout">
          <Sidebar workspaceId={activeId} />
          <Chat workspaceId={activeId} />
        </div>
      ) : (
        <div className="center">
          <div className="card">
            <p className="hint">Create a workspace to get started.</p>
            <button className="btn" onClick={onCreateWorkspace}>
              Create workspace
            </button>
          </div>
        </div>
      )}
    </div>
  );
}
