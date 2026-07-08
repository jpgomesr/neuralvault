import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { listSourceFiles, listSources, uploadSource } from "@/lib/api/sources";

export const sourcesQueryKey = (workspaceId: string) => ["sources", workspaceId] as const;

export const sourceFilesQueryKey = (sourceId: string) =>
  ["sources", sourceId, "files"] as const;

/** useSources lists a workspace's sources. */
export function useSources(workspaceId: string) {
  return useQuery({
    queryKey: sourcesQueryKey(workspaceId),
    queryFn: () => listSources(workspaceId),
  });
}

/** useSourceFiles lists a source's original files. Only fetches when enabled. */
export function useSourceFiles(sourceId: string, enabled = true) {
  return useQuery({
    queryKey: sourceFilesQueryKey(sourceId),
    queryFn: () => listSourceFiles(sourceId),
    enabled: enabled && sourceId !== "",
  });
}

/** useUploadSourceMutation uploads a source and invalidates its workspace's list. */
export function useUploadSourceMutation(workspaceId: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ name, files }: { name: string; files: FileList }) =>
      uploadSource(workspaceId, name, files),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: sourcesQueryKey(workspaceId) });
    },
  });
}
