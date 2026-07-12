import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { createConversation, listConversationMessages, listConversations } from "@/lib/api/conversations";

export const conversationListQueryKey = (workspaceId: string) => ["conversations", workspaceId] as const;

export const conversationMessagesQueryKey = (conversationId: string) =>
  ["conversations", conversationId, "messages"] as const;

/** useConversationList lists a workspace's persisted conversations. */
export function useConversationList(workspaceId: string) {
  return useQuery({
    queryKey: conversationListQueryKey(workspaceId),
    queryFn: () => listConversations(workspaceId),
    enabled: workspaceId !== "",
  });
}

/** useConversationMessages lists a conversation's persisted messages. Only fetches when enabled. */
export function useConversationMessages(conversationId: string, enabled = true) {
  return useQuery({
    queryKey: conversationMessagesQueryKey(conversationId),
    queryFn: () => listConversationMessages(conversationId),
    enabled: enabled && conversationId !== "",
  });
}

/** useCreateConversationMutation creates a conversation and invalidates its workspace's list. */
export function useCreateConversationMutation(workspaceId: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: () => createConversation(workspaceId),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: conversationListQueryKey(workspaceId) });
    },
  });
}
