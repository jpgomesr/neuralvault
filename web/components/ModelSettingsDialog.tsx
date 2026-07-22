"use client";

import { useState } from "react";
import { AlertTriangle, Check, Trash2 } from "lucide-react";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import {
  useDeleteCredentialMutation,
  useModelSettings,
  useModels,
  useProviders,
  useReindexMutation,
  useSaveCredentialMutation,
  useSetEmbeddingMutation,
  useSetLLMMutation,
} from "@/hooks/use-models";
import type { EmbeddingChange, ModelPurpose, Provider, ProviderStatus } from "@/lib/api/models";

/**
 * ModelSettingsDialog is where a workspace brings its own API key (BYOK) and
 * picks which model answers queries and which one embeds its sources.
 */
export default function ModelSettingsDialog({
  workspaceId,
  open,
  onOpenChange,
}: {
  workspaceId: string;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const providers = useProviders(open ? workspaceId : null);
  const settings = useModelSettings(open ? workspaceId : null);

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-lg">
        <DialogHeader>
          <DialogTitle>Model settings</DialogTitle>
          <DialogDescription>
            Bring your own API key to run this workspace on a hosted provider instead of
            the local Ollama server.
          </DialogDescription>
        </DialogHeader>

        {providers.isLoading || settings.isLoading ? (
          <div className="text-sm text-muted-foreground">Loading…</div>
        ) : providers.isError ? (
          <div className="error">{(providers.error as Error).message}</div>
        ) : (
          <div className="flex flex-col gap-6">
            <ApiKeysSection workspaceId={workspaceId} providers={providers.data ?? []} />
            <ModelPickers
              workspaceId={workspaceId}
              providers={providers.data ?? []}
              currentLLM={settings.data?.llm_provider}
              currentLLMModel={settings.data?.llm_model}
              currentEmbedding={settings.data?.embedding_provider}
              currentEmbeddingModel={settings.data?.embedding_model}
            />
          </div>
        )}
      </DialogContent>
    </Dialog>
  );
}

/** ApiKeysSection stores and removes the workspace's provider API keys. */
function ApiKeysSection({
  workspaceId,
  providers,
}: {
  workspaceId: string;
  providers: ProviderStatus[];
}) {
  const saveCredential = useSaveCredentialMutation(workspaceId);
  const deleteCredential = useDeleteCredentialMutation(workspaceId);

  const [selected, setSelected] = useState<Provider | "">("");
  const [apiKey, setApiKey] = useState("");
  const [error, setError] = useState<string | null>(null);

  // Ollama is server-local and unauthenticated, so it never takes a key.
  const keyedProviders = providers.filter((p) => p.requires_api_key);
  const configured = keyedProviders.filter((p) => p.configured);

  async function onSave(e: React.FormEvent) {
    e.preventDefault();
    if (!selected || !apiKey.trim()) return;
    setError(null);
    try {
      await saveCredential.mutateAsync({ provider: selected, apiKey: apiKey.trim() });
      setApiKey("");
      setSelected("");
    } catch (err) {
      // The API validates the key against the provider before storing it, so
      // this message is the provider's own rejection ("Invalid API Key").
      setError(err instanceof Error ? err.message : "failed to save key");
    }
  }

  return (
    <section className="flex flex-col gap-3">
      <div>
        <h3 className="text-sm font-medium">API keys</h3>
        <p className="text-xs text-muted-foreground">
          Keys are encrypted at rest and never shown again after saving.
        </p>
      </div>

      {configured.length > 0 && (
        <ul className="flex flex-col gap-1.5">
          {configured.map((p) => (
            <li
              key={p.provider}
              className="flex items-center gap-2 rounded-md border px-2.5 py-1.5 text-sm"
            >
              <Check className="size-3.5 text-muted-foreground" />
              <span className="flex-1">{p.name}</span>
              <span className="font-mono text-xs text-muted-foreground">
                ••••{p.api_key_hint}
              </span>
              <Button
                type="button"
                variant="ghost"
                size="sm"
                aria-label={`Remove ${p.name} key`}
                disabled={deleteCredential.isPending}
                onClick={() => deleteCredential.mutate(p.provider)}
              >
                <Trash2 className="size-3.5" />
              </Button>
            </li>
          ))}
        </ul>
      )}

      <form className="flex items-end gap-2" onSubmit={onSave}>
        <div className="flex flex-1 flex-col gap-1.5">
          <Label htmlFor="provider">Provider</Label>
          <Select value={selected} onValueChange={(v) => setSelected(v as Provider)}>
            <SelectTrigger id="provider">
              <SelectValue placeholder="Select a provider" />
            </SelectTrigger>
            <SelectContent>
              {keyedProviders.map((p) => (
                <SelectItem key={p.provider} value={p.provider}>
                  {p.name}
                  {p.free_tier && " — free tier"}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>
        <div className="flex flex-1 flex-col gap-1.5">
          <Label htmlFor="api-key">API key</Label>
          <Input
            id="api-key"
            type="password"
            autoComplete="off"
            placeholder="Paste your key"
            value={apiKey}
            onChange={(e) => setApiKey(e.target.value)}
          />
        </div>
        <Button type="submit" disabled={!selected || !apiKey.trim() || saveCredential.isPending}>
          {saveCredential.isPending ? "Verifying…" : "Save"}
        </Button>
      </form>

      {error && <div className="error">{error}</div>}
    </section>
  );
}

/** ModelPickers selects the completion and embedding models. */
function ModelPickers({
  workspaceId,
  providers,
  currentLLM,
  currentLLMModel,
  currentEmbedding,
  currentEmbeddingModel,
}: {
  workspaceId: string;
  providers: ProviderStatus[];
  currentLLM?: Provider;
  currentLLMModel?: string;
  currentEmbedding?: Provider;
  currentEmbeddingModel?: string;
}) {
  const setLLM = useSetLLMMutation(workspaceId);
  const setEmbedding = useSetEmbeddingMutation(workspaceId);
  const reindex = useReindexMutation(workspaceId);

  const [llmProvider, setLLMProvider] = useState<Provider | "">(currentLLM ?? "");
  const [embProvider, setEmbProvider] = useState<Provider | "">(currentEmbedding ?? "");
  const [change, setChange] = useState<EmbeddingChange | null>(null);
  const [error, setError] = useState<string | null>(null);

  const usable = providers.filter((p) => p.configured);
  const llmProviders = usable.filter((p) => p.supports_completions);
  // Anthropic, Groq, OpenRouter and GitHub Models serve no embeddings, so they
  // must never appear here — selecting one would fail at index time.
  const embProviders = usable.filter((p) => p.supports_embeddings);

  return (
    <section className="flex flex-col gap-4">
      <h3 className="text-sm font-medium">Models</h3>

      <ModelPicker
        label="Completion"
        description="Answers your questions."
        purpose="completion"
        workspaceId={workspaceId}
        providers={llmProviders}
        provider={llmProvider}
        onProviderChange={setLLMProvider}
        currentModel={currentLLMModel}
        pending={setLLM.isPending}
        onSelect={async (provider, model) => {
          setError(null);
          try {
            await setLLM.mutateAsync({ provider, model });
          } catch (err) {
            setError(err instanceof Error ? err.message : "failed to set completion model");
          }
        }}
      />

      <ModelPicker
        label="Embedding"
        description="Indexes your sources. Changing it requires a re-index."
        purpose="embedding"
        workspaceId={workspaceId}
        providers={embProviders}
        provider={embProvider}
        onProviderChange={setEmbProvider}
        currentModel={currentEmbeddingModel}
        pending={setEmbedding.isPending}
        onSelect={async (provider, model) => {
          setError(null);
          try {
            setChange(await setEmbedding.mutateAsync({ provider, model }));
          } catch (err) {
            setError(err instanceof Error ? err.message : "failed to set embedding model");
          }
        }}
      />

      {change?.reindex_required && (
        <div className="flex items-start gap-2 rounded-md border border-amber-500/40 bg-amber-500/10 p-3 text-sm">
          <AlertTriangle className="mt-0.5 size-4 shrink-0 text-amber-600" />
          <div className="flex flex-col items-start gap-2">
            <p>
              {change.stale_sources} source{change.stale_sources === 1 ? "" : "s"} still hold
              vectors from the previous embedding model. Until they are re-indexed, this
              workspace will find nothing.
            </p>
            <Button
              type="button"
              size="sm"
              disabled={reindex.isPending || reindex.isSuccess}
              onClick={() => reindex.mutate()}
            >
              {reindex.isSuccess
                ? `Re-indexing ${reindex.data?.queued ?? 0} source(s)…`
                : reindex.isPending
                  ? "Queueing…"
                  : "Re-index now"}
            </Button>
          </div>
        </div>
      )}

      {error && <div className="error">{error}</div>}
    </section>
  );
}

/** ModelPicker is a provider dropdown plus that provider's live model list. */
function ModelPicker({
  label,
  description,
  purpose,
  workspaceId,
  providers,
  provider,
  onProviderChange,
  currentModel,
  pending,
  onSelect,
}: {
  label: string;
  description: string;
  purpose: ModelPurpose;
  workspaceId: string;
  providers: ProviderStatus[];
  provider: Provider | "";
  onProviderChange: (p: Provider) => void;
  currentModel?: string;
  pending: boolean;
  onSelect: (provider: Provider, model: string) => void;
}) {
  const models = useModels(workspaceId, provider || null, purpose, !!provider);

  return (
    <div className="flex flex-col gap-1.5">
      <Label>{label}</Label>
      <p className="text-xs text-muted-foreground">{description}</p>
      <div className="flex gap-2">
        <Select value={provider} onValueChange={(v) => onProviderChange(v as Provider)}>
          <SelectTrigger className="flex-1" aria-label={`${label} provider`}>
            <SelectValue placeholder="Provider" />
          </SelectTrigger>
          <SelectContent>
            {providers.map((p) => (
              <SelectItem key={p.provider} value={p.provider}>
                {p.name}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>

        <Select
          value={currentModel ?? ""}
          disabled={!provider || models.isLoading || pending}
          onValueChange={(model) => provider && onSelect(provider, model)}
        >
          <SelectTrigger className="flex-1" aria-label={`${label} model`}>
            <SelectValue placeholder={models.isLoading ? "Loading…" : "Model"} />
          </SelectTrigger>
          <SelectContent>
            {(models.data ?? []).map((m) => (
              <SelectItem key={m.id} value={m.id}>
                {m.name}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </div>
      {models.isError && <div className="error">{(models.error as Error).message}</div>}
    </div>
  );
}
