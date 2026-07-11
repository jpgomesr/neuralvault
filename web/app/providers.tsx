"use client";

import { QueryClientProvider } from "@tanstack/react-query";
import type { ReactNode } from "react";
import { ConversationProvider } from "@/lib/conversation-context";
import { queryClient } from "@/lib/query-client";
import { WorkspaceProvider } from "@/lib/workspace-context";

export default function Providers({ children }: { children: ReactNode }) {
  return (
    <QueryClientProvider client={queryClient}>
      <WorkspaceProvider>
        <ConversationProvider>{children}</ConversationProvider>
      </WorkspaceProvider>
    </QueryClientProvider>
  );
}
