"use client";

import { useEffect, useState } from "react";
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
import { useLogoutMutation, useMe } from "@/hooks/use-me";
import { useCreateWorkspaceMutation, useWorkspaces } from "@/hooks/use-workspaces";

export default function Home() {
  const { me } = useMe();
  const { data: workspaces = [] } = useWorkspaces(!!me);
  const createWorkspaceMutation = useCreateWorkspaceMutation();
  const logoutMutation = useLogoutMutation();
  const [activeId, setActiveId] = useState<string>("");

  useEffect(() => {
    // eslint-disable-next-line react-hooks/set-state-in-effect -- defaults to the first workspace once the list loads
    setActiveId((prev) => prev || workspaces[0]?.ID || "");
  }, [workspaces]);

  function onCreateWorkspace() {
    const name = window.prompt("New workspace name");
    if (!name) return;
    createWorkspaceMutation.mutate(name, { onSuccess: (ws) => setActiveId(ws.ID) });
  }

  function onLogout() {
    logoutMutation.mutate(undefined, { onSuccess: () => setActiveId("") });
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
