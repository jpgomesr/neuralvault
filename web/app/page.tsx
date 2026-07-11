"use client";

import Link from "next/link";
import { useState } from "react";
import Chat from "@/components/Chat";
import ChatList from "@/components/ChatList";
import CreateWorkspaceDialog from "@/components/CreateWorkspaceDialog";
import Sidebar from "@/components/Sidebar";
import SignIn from "@/components/SignIn";
import WorkspaceSwitcher from "@/components/WorkspaceSwitcher";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { useLogoutMutation, useMe } from "@/hooks/use-me";
import { useActiveWorkspace } from "@/lib/workspace-context";

export default function Home() {
  const { me } = useMe();
  const { activeId, setActiveId, isLoading: workspacesLoading } = useActiveWorkspace();
  const logoutMutation = useLogoutMutation();
  const [createOpen, setCreateOpen] = useState(false);

  function onLogout() {
    logoutMutation.mutate(undefined, { onSuccess: () => setActiveId("") });
  }

  if (me === undefined) return <div className="center hint">Loading…</div>;
  if (me === null) return <SignIn />;
  if (workspacesLoading) return <div className="center hint">Loading…</div>;

  return (
    <div className="app">
      <div className="topbar">
        <span className="brand">NeuralVault</span>
        <WorkspaceSwitcher />
        <Button variant="ghost" asChild>
          <Link href="/sources">Sources</Link>
        </Button>
        <div className="spacer" />
        <span className="email">{me.email}</span>
        <Button variant="secondary" onClick={onLogout}>
          Sign out
        </Button>
      </div>

      {activeId ? (
        <div className="layout">
          <aside className="sidebar">
            <Tabs defaultValue="chats">
              <TabsList className="w-full">
                <TabsTrigger value="chats">Chats</TabsTrigger>
                <TabsTrigger value="sources">Sources</TabsTrigger>
              </TabsList>
              <TabsContent value="chats">
                <ChatList />
              </TabsContent>
              <TabsContent value="sources">
                <Sidebar workspaceId={activeId} />
              </TabsContent>
            </Tabs>
          </aside>
          <Chat workspaceId={activeId} />
        </div>
      ) : (
        <div className="center">
          <Card className="items-center p-6 text-center">
            <p className="hint">Create a workspace to get started.</p>
            <Button onClick={() => setCreateOpen(true)}>Create workspace</Button>
          </Card>
          <CreateWorkspaceDialog
            open={createOpen}
            onOpenChange={setCreateOpen}
            onCreated={(ws) => setActiveId(ws.ID)}
          />
        </div>
      )}
    </div>
  );
}
