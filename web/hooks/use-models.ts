import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  deleteCredential,
  getModelSettings,
  listModels,
  listProviders,
  reindexWorkspace,
  saveCredential,
  setEmbeddingSettings,
  setLLMSettings,
  type ModelPurpose,
  type Provider,
} from "@/lib/api/models";

export const providersQueryKey = (workspaceId: string) => ["providers", workspaceId] as const;
// purpose is part of the key, not just an argument: Gemini can be configured
// as both the completion and the embedding provider at once, and each
// purpose gets a differently-filtered list — without purpose in the key the
// two pickers' queries would collide and overwrite each other's cache entry.
export const modelsQueryKey = (workspaceId: string, provider: Provider, purpose: ModelPurpose) =>
  ["models", workspaceId, provider, purpose] as const;
export const modelSettingsQueryKey = (workspaceId: string) => ["model-settings", workspaceId] as const;

/** useProviders lists the provider catalog with this workspace's credential state. */
export function useProviders(workspaceId: string | null) {
  return useQuery({
    queryKey: providersQueryKey(workspaceId ?? ""),
    queryFn: () => listProviders(workspaceId!),
    enabled: !!workspaceId,
  });
}

/**
 * useModels lists a provider's models, live from the provider.
 *
 * It stays disabled until the workspace holds a credential for the provider:
 * without one the request is a guaranteed error, and firing it would surface a
 * failure the user has not caused yet.
 */
export function useModels(
  workspaceId: string | null,
  provider: Provider | null,
  purpose: ModelPurpose,
  configured = true,
) {
  return useQuery({
    queryKey: modelsQueryKey(workspaceId ?? "", provider ?? "ollama", purpose),
    queryFn: () => listModels(workspaceId!, provider!, purpose),
    enabled: !!workspaceId && !!provider && configured,
    // Provider model lists change rarely; refetching them on every dropdown
    // open would spend a round-trip (and a rate-limit slot) for nothing.
    staleTime: 5 * 60 * 1000,
    retry: false,
  });
}

/** useModelSettings returns the workspace's chosen models. */
export function useModelSettings(workspaceId: string | null) {
  return useQuery({
    queryKey: modelSettingsQueryKey(workspaceId ?? ""),
    queryFn: () => getModelSettings(workspaceId!),
    enabled: !!workspaceId,
  });
}

/**
 * useSaveCredentialMutation stores an API key. It invalidates the provider list
 * so the newly configured provider immediately becomes selectable.
 */
export function useSaveCredentialMutation(workspaceId: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ provider, apiKey, baseUrl }: { provider: Provider; apiKey: string; baseUrl?: string }) =>
      saveCredential(workspaceId, provider, apiKey, baseUrl),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: providersQueryKey(workspaceId) });
    },
  });
}

export function useDeleteCredentialMutation(workspaceId: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (provider: Provider) => deleteCredential(workspaceId, provider),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: providersQueryKey(workspaceId) });
      void queryClient.invalidateQueries({ queryKey: modelSettingsQueryKey(workspaceId) });
    },
  });
}

export function useSetLLMMutation(workspaceId: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ provider, model }: { provider: Provider; model: string }) =>
      setLLMSettings(workspaceId, provider, model),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: modelSettingsQueryKey(workspaceId) });
    },
  });
}

/**
 * useSetEmbeddingMutation changes the embedding model. Its result carries
 * stale_sources — the caller must surface it, since retrieval returns nothing
 * until those sources are re-indexed.
 */
export function useSetEmbeddingMutation(workspaceId: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ provider, model }: { provider: Provider; model: string }) =>
      setEmbeddingSettings(workspaceId, provider, model),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: modelSettingsQueryKey(workspaceId) });
    },
  });
}

export function useReindexMutation(workspaceId: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: () => reindexWorkspace(workspaceId),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ["sources", workspaceId] });
    },
  });
}
