import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { createWorkspace, listWorkspaces } from "@/lib/api/workspaces";

export const workspacesQueryKey = ["workspaces"] as const;

/** useWorkspaces lists the authenticated user's workspaces. */
export function useWorkspaces(enabled = true) {
  return useQuery({ queryKey: workspacesQueryKey, queryFn: listWorkspaces, enabled });
}

/** useCreateWorkspaceMutation creates a workspace and invalidates the list. */
export function useCreateWorkspaceMutation() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (name: string) => createWorkspace(name),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: workspacesQueryKey });
    },
  });
}
