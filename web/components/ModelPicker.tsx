"use client";

import { useMemo } from "react";
import { useQueries } from "@tanstack/react-query";
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectLabel,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { modelsQueryKey, useModelSettings, useProviders } from "@/hooks/use-models";
import { listModels, type ModelInfo, type Provider } from "@/lib/api/models";
import type { ModelOverride } from "@/lib/api/query";

/**
 * ModelPicker switches the model answering the next question, for one question
 * only — it sends a per-request override rather than saving a setting, so
 * trying another model does not change the workspace's default or affect other
 * conversations.
 *
 * The persistent default is set in the workspace's model settings dialog.
 */
export default function ModelPicker({
  workspaceId,
  value,
  onChange,
  disabled,
}: {
  workspaceId: string;
  value: ModelOverride | null;
  onChange: (override: ModelOverride | null) => void;
  disabled?: boolean;
}) {
  const providers = useProviders(workspaceId);
  const settings = useModelSettings(workspaceId);

  // Only providers holding a key can be picked; an override is a choice among
  // configured providers, not a way around configuration.
  const usable = useMemo(
    () => (providers.data ?? []).filter((p) => p.configured && p.supports_completions),
    [providers.data],
  );

  const models = useModelsForProviders(workspaceId, usable.map((p) => p.provider));

  const defaultLabel = settings.data?.llm_model ?? "Workspace default";

  // The option value encodes both halves, since a model ID alone is ambiguous
  // across providers (OpenRouter and Groq both serve "llama-3.3-70b").
  const selected = value ? `${value.provider}:${value.model}` : "";

  return (
    <Select
      value={selected}
      disabled={disabled}
      onValueChange={(v) => {
        if (!v) return onChange(null);
        const [provider, ...rest] = v.split(":");
        onChange({ provider: provider as Provider, model: rest.join(":") });
      }}
    >
      <SelectTrigger className="w-44 shrink-0" aria-label="Model">
        <SelectValue placeholder={defaultLabel} />
      </SelectTrigger>
      <SelectContent>
        {usable.map((p) => {
          const list = models[p.provider] ?? [];
          if (list.length === 0) return null;
          return (
            <SelectGroup key={p.provider}>
              <SelectLabel>{p.name}</SelectLabel>
              {list.map((m) => (
                <SelectItem key={`${p.provider}:${m.id}`} value={`${p.provider}:${m.id}`}>
                  {m.name}
                </SelectItem>
              ))}
            </SelectGroup>
          );
        })}
      </SelectContent>
    </Select>
  );
}

/**
 * useModelsForProviders fetches every configured provider's model list at once.
 *
 * useQueries, not a useModels call per provider: the number of configured
 * providers changes when a key is added or removed, and a hook count that varies
 * between renders breaks the rules of hooks. The queries share the cache keys
 * useModels uses, so the settings dialog and this picker never double-fetch.
 */
function useModelsForProviders(workspaceId: string, providers: Provider[]) {
  const results = useQueries({
    queries: providers.map((provider) => ({
      queryKey: modelsQueryKey(workspaceId, provider),
      queryFn: () => listModels(workspaceId, provider),
      staleTime: 5 * 60 * 1000,
      retry: false,
    })),
  });

  const byProvider: Partial<Record<Provider, ModelInfo[]>> = {};
  providers.forEach((provider, i) => {
    byProvider[provider] = results[i]?.data ?? [];
  });
  return byProvider;
}
