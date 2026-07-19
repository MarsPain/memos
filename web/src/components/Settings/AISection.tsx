import { create } from "@bufbuild/protobuf";
import { isEqual } from "lodash-es";
import { MoreVerticalIcon, PlusIcon } from "lucide-react";
import { useEffect, useMemo, useRef, useState } from "react";
import { toast } from "react-hot-toast";
import ConfirmDialog from "@/components/ConfirmDialog";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger } from "@/components/ui/dropdown-menu";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Switch } from "@/components/ui/switch";
import { Textarea } from "@/components/ui/textarea";
import { instanceServiceClient } from "@/connect";
import { useInstance } from "@/contexts/InstanceContext";
import {
  InstanceSetting_AICapability,
  InstanceSetting_AIProviderConfig,
  InstanceSetting_AIProviderConfigSchema,
  InstanceSetting_AIProviderType,
  InstanceSetting_AISettingSchema,
  InstanceSetting_CapabilityReadinessSchema,
  InstanceSetting_CapabilityState,
  InstanceSetting_EmbeddingConfig,
  InstanceSetting_EmbeddingConfigSchema,
  InstanceSetting_GenerationConfig,
  InstanceSetting_GenerationConfigSchema,
  InstanceSetting_Key,
  InstanceSetting_TranscriptionConfig,
  InstanceSetting_TranscriptionConfigSchema,
  InstanceSettingSchema,
} from "@/types/proto/api/v1/instance_service_pb";
import { useTranslate } from "@/utils/i18n";
import { type CapabilityDraft, validateCapabilityDraft } from "./ai-setting-state";
import SettingGroup from "./SettingGroup";
import { SettingPanel } from "./SettingList";
import SettingSection from "./SettingSection";
import SettingTable from "./SettingTable";
import useInstanceSettingUpdater, { buildInstanceSettingName } from "./useInstanceSettingUpdater";

type LocalAIProvider = {
  id: string;
  title: string;
  type: InstanceSetting_AIProviderType;
  endpoint: string;
  apiKey: string;
  apiKeySet: boolean;
  apiKeyHint: string;
  allowPrivateNetwork: boolean;
};

type LocalCapability = CapabilityDraft;

type LocalTranscription = {
  providerId: string;
  model: string;
  language: string;
  prompt: string;
};

const providerTypeOptions = [InstanceSetting_AIProviderType.OPENAI, InstanceSetting_AIProviderType.GEMINI];

const byokNotes = ["setting.ai.byok-key-note", "setting.ai.byok-storage-note", "setting.ai.byok-model-note"] as const;

const createProviderID = () => {
  if (typeof crypto !== "undefined" && "randomUUID" in crypto) {
    return crypto.randomUUID();
  }
  return `ai-${Date.now()}-${Math.random().toString(36).slice(2, 8)}`;
};

const getProviderTypeLabel = (type: InstanceSetting_AIProviderType) => {
  return InstanceSetting_AIProviderType[type] ?? "UNKNOWN";
};

const providerTypeSelectOptions = providerTypeOptions.map((type) => ({ value: String(type), label: getProviderTypeLabel(type) }));

const toLocalProvider = (provider: InstanceSetting_AIProviderConfig): LocalAIProvider => ({
  id: provider.id,
  title: provider.title,
  type: provider.type,
  endpoint: provider.endpoint,
  apiKey: "",
  apiKeySet: provider.apiKeySet,
  apiKeyHint: provider.apiKeyHint,
  allowPrivateNetwork: provider.allowPrivateNetwork,
});

const toLocalCapability = (config: { providerId: string; model: string; dimensions?: number } | undefined): LocalCapability => ({
  providerId: config?.providerId ?? "",
  model: config?.model ?? "",
  dimensions: config?.dimensions ?? 0,
});

const toLocalTranscription = (config: InstanceSetting_TranscriptionConfig | undefined): LocalTranscription => ({
  providerId: config?.providerId ?? "",
  model: config?.model ?? "",
  language: config?.language ?? "",
  prompt: config?.prompt ?? "",
});

const newProvider = (): LocalAIProvider => ({
  id: createProviderID(),
  title: "",
  type: InstanceSetting_AIProviderType.OPENAI,
  endpoint: "",
  apiKey: "",
  apiKeySet: false,
  apiKeyHint: "",
  allowPrivateNetwork: false,
});

const toProviderConfig = (provider: LocalAIProvider) =>
  create(InstanceSetting_AIProviderConfigSchema, {
    id: provider.id,
    title: provider.title.trim(),
    type: provider.type,
    endpoint: provider.endpoint.trim(),
    apiKey: provider.apiKey,
    allowPrivateNetwork: provider.allowPrivateNetwork,
  });

const toTranscriptionConfig = (transcription: LocalTranscription) =>
  create(InstanceSetting_TranscriptionConfigSchema, {
    providerId: transcription.providerId,
    model: transcription.model.trim(),
    language: transcription.language.trim(),
    prompt: transcription.prompt,
  });

const toGenerationConfig = (generation: LocalCapability) =>
  create(InstanceSetting_GenerationConfigSchema, { providerId: generation.providerId, model: generation.model.trim() });

const toEmbeddingConfig = (embedding: LocalCapability) =>
  create(InstanceSetting_EmbeddingConfigSchema, {
    providerId: embedding.providerId,
    model: embedding.model.trim(),
    dimensions: embedding.dimensions,
  });

const AISection = () => {
  const t = useTranslate();
  const saveInstanceSetting = useInstanceSettingUpdater();
  const { aiSetting: originalSetting } = useInstance();
  const [providers, setProviders] = useState<LocalAIProvider[]>(() => originalSetting.providers.map(toLocalProvider));
  const [transcription, setTranscription] = useState<LocalTranscription>(() => toLocalTranscription(originalSetting.transcription));
  const [generation, setGeneration] = useState<LocalCapability>(() => toLocalCapability(originalSetting.generation));
  const [embedding, setEmbedding] = useState<LocalCapability>(() => toLocalCapability(originalSetting.embedding));
  const [externalProcessingAcknowledged, setExternalProcessingAcknowledged] = useState(originalSetting.externalProcessingAcknowledged);
  const [testingCapability, setTestingCapability] = useState<InstanceSetting_AICapability>();
  const [readiness, setReadiness] = useState(() => originalSetting.readiness);
  const [editingProvider, setEditingProvider] = useState<LocalAIProvider | undefined>();
  const [deleteTarget, setDeleteTarget] = useState<LocalAIProvider | undefined>();

  useEffect(() => {
    setProviders(originalSetting.providers.map(toLocalProvider));
  }, [originalSetting.providers]);

  // Only re-sync the transcription draft when the server-side content actually
  // changes — not on every originalSetting identity change. This prevents
  // provider-side saves (which keep transcription unchanged on the server) from
  // wiping an in-progress transcription draft.
  const lastSyncedTranscription = useRef<LocalTranscription>(toLocalTranscription(originalSetting.transcription));
  useEffect(() => {
    const next = toLocalTranscription(originalSetting.transcription);
    if (!isEqual(lastSyncedTranscription.current, next)) {
      setTranscription(next);
      lastSyncedTranscription.current = next;
    }
  }, [originalSetting.transcription]);

  useEffect(() => {
    setGeneration(toLocalCapability(originalSetting.generation));
  }, [originalSetting.generation]);

  useEffect(() => {
    setEmbedding(toLocalCapability(originalSetting.embedding));
    setExternalProcessingAcknowledged(originalSetting.externalProcessingAcknowledged);
  }, [originalSetting.embedding, originalSetting.externalProcessingAcknowledged]);

  useEffect(() => setReadiness(originalSetting.readiness), [originalSetting.readiness]);

  const originalTranscription = useMemo(() => toLocalTranscription(originalSetting.transcription), [originalSetting.transcription]);
  const transcriptionHasChanges = !isEqual(transcription, originalTranscription);
  const originalGeneration = useMemo(() => toLocalCapability(originalSetting.generation), [originalSetting.generation]);
  const originalEmbedding = useMemo(() => toLocalCapability(originalSetting.embedding), [originalSetting.embedding]);
  const generationHasChanges = !isEqual(generation, originalGeneration);
  const embeddingHasChanges =
    !isEqual(embedding, originalEmbedding) || externalProcessingAcknowledged !== originalSetting.externalProcessingAcknowledged;

  const transcriptionProviderRef = useMemo(
    () => providers.find((provider) => provider.id === transcription.providerId),
    [providers, transcription.providerId],
  );

  // Persists the AI setting using a specific providers list and transcription
  // value. Provider operations pass originalSetting.transcription so an
  // in-progress transcription draft is never accidentally committed.
  const persistAISetting = async (
    nextProviders: LocalAIProvider[],
    nextTranscription: InstanceSetting_TranscriptionConfig | undefined,
    nextGeneration: InstanceSetting_GenerationConfig | undefined,
    nextEmbedding: InstanceSetting_EmbeddingConfig | undefined,
    nextExternalProcessingAcknowledged: boolean,
    errorContext: string,
  ) => {
    return saveInstanceSetting({
      key: InstanceSetting_Key.AI,
      setting: create(InstanceSettingSchema, {
        name: buildInstanceSettingName(InstanceSetting_Key.AI),
        value: {
          case: "aiSetting",
          value: create(InstanceSetting_AISettingSchema, {
            providers: nextProviders.map(toProviderConfig),
            transcription: nextTranscription,
            generation: nextGeneration,
            embedding: nextEmbedding,
            externalProcessingAcknowledged: nextExternalProcessingAcknowledged,
          }),
        },
      }),
      errorContext,
    });
  };

  const handleCreateProvider = () => {
    setEditingProvider(newProvider());
  };

  const handleEditProvider = (provider: LocalAIProvider) => {
    setEditingProvider({ ...provider, apiKey: "" });
  };

  const handleSaveProvider = async (provider: LocalAIProvider) => {
    const title = provider.title.trim();
    const endpoint = provider.endpoint.trim();

    if (!title) {
      toast.error(t("setting.ai.provider-title-required"));
      return;
    }
    if (!provider.apiKeySet && !provider.apiKey.trim()) {
      toast.error(t("setting.ai.api-key-required"));
      return;
    }

    const normalizedProvider = { ...provider, title, endpoint };
    const exists = providers.some((item) => item.id === normalizedProvider.id);
    const nextProviders = exists
      ? providers.map((item) => (item.id === normalizedProvider.id ? normalizedProvider : item))
      : [...providers, normalizedProvider];

    const ok = await persistAISetting(
      nextProviders,
      originalSetting.transcription,
      originalSetting.generation,
      originalSetting.embedding,
      originalSetting.externalProcessingAcknowledged,
      "Update AI provider",
    );
    if (!ok) return;
    setProviders(nextProviders);
    setEditingProvider(undefined);
  };

  const handleDeleteProvider = async () => {
    if (!deleteTarget) return;
    const target = deleteTarget;
    const nextProviders = providers.filter((provider) => provider.id !== target.id);

    // If the persisted transcription references the deleted provider, the
    // server would reject the save (provider_id must reference an existing
    // provider). Send a cleared transcription in that case.
    const persistedTranscription = originalSetting.transcription;
    const nextTranscription =
      persistedTranscription && persistedTranscription.providerId === target.id
        ? create(InstanceSetting_TranscriptionConfigSchema, {})
        : persistedTranscription;

    const nextGeneration =
      originalSetting.generation?.providerId === target.id
        ? create(InstanceSetting_GenerationConfigSchema, {})
        : originalSetting.generation;
    const nextEmbedding =
      originalSetting.embedding?.providerId === target.id ? create(InstanceSetting_EmbeddingConfigSchema, {}) : originalSetting.embedding;

    const ok = await persistAISetting(
      nextProviders,
      nextTranscription,
      nextGeneration,
      nextEmbedding,
      originalSetting.externalProcessingAcknowledged,
      "Delete AI provider",
    );
    if (!ok) return;
    setProviders(nextProviders);
    if (transcription.providerId === target.id) {
      setTranscription((prev) => ({ ...prev, providerId: "" }));
    }
    if (generation.providerId === target.id) setGeneration((previous) => ({ ...previous, providerId: "" }));
    if (embedding.providerId === target.id) setEmbedding((previous) => ({ ...previous, providerId: "" }));
    setDeleteTarget(undefined);
  };

  const handleSaveTranscription = async () => {
    if (transcription.providerId && !transcriptionProviderRef) {
      toast.error(t("setting.ai.transcription-empty-providers"));
      return;
    }
    await persistAISetting(
      providers,
      toTranscriptionConfig(transcription),
      originalSetting.generation,
      originalSetting.embedding,
      originalSetting.externalProcessingAcknowledged,
      "Update transcription",
    );
  };

  const showCapabilityValidationError = (error: ReturnType<typeof validateCapabilityDraft>) => {
    if (error) toast.error(t(`setting.ai.${error}`));
  };

  const handleSaveGeneration = async () => {
    const validationError = validateCapabilityDraft(generation, false);
    if (validationError) {
      showCapabilityValidationError(validationError);
      return;
    }
    await persistAISetting(
      providers,
      originalSetting.transcription,
      toGenerationConfig(generation),
      originalSetting.embedding,
      originalSetting.externalProcessingAcknowledged,
      "Update generation capability",
    );
  };

  const handleSaveEmbedding = async () => {
    const validationError = validateCapabilityDraft(embedding, true, externalProcessingAcknowledged);
    if (validationError) {
      showCapabilityValidationError(validationError);
      return;
    }
    await persistAISetting(
      providers,
      originalSetting.transcription,
      originalSetting.generation,
      toEmbeddingConfig(embedding),
      externalProcessingAcknowledged,
      "Update embedding capability",
    );
  };

  const handleTestCapability = async (capability: InstanceSetting_AICapability, draft: LocalCapability) => {
    const provider = providers.find((item) => item.id === draft.providerId);
    if (!provider || !draft.model.trim()) {
      toast.error(t("setting.ai.capability-incomplete"));
      return;
    }
    setTestingCapability(capability);
    try {
      const result = await instanceServiceClient.testInstanceAISetting({
        provider: toProviderConfig(provider),
        capability,
        model: draft.model.trim(),
        dimensions: draft.dimensions,
      });
      if (result.ready) {
        toast.success(t("setting.ai.test-succeeded"));
      } else {
        toast.error(result.message || t("setting.ai.test-failed"));
      }
      const state = result.ready ? InstanceSetting_CapabilityState.READY : InstanceSetting_CapabilityState.UNAVAILABLE;
      setReadiness((previous) =>
        create(InstanceSetting_CapabilityReadinessSchema, {
          textGeneration: previous?.textGeneration,
          streaming: previous?.streaming,
          structuredTools: previous?.structuredTools,
          embeddings: previous?.embeddings,
          ...(capability === InstanceSetting_AICapability.TEXT_GENERATION ? { textGeneration: state } : {}),
          ...(capability === InstanceSetting_AICapability.STREAMING ? { streaming: state } : {}),
          ...(capability === InstanceSetting_AICapability.STRUCTURED_TOOLS ? { structuredTools: state } : {}),
          ...(capability === InstanceSetting_AICapability.EMBEDDINGS ? { embeddings: state } : {}),
        }),
      );
    } catch {
      toast.error(t("setting.ai.test-failed"));
    } finally {
      setTestingCapability(undefined);
    }
  };

  return (
    <SettingSection
      title={t("setting.ai.label")}
      actions={
        <Button onClick={handleCreateProvider}>
          <PlusIcon className="w-4 h-4 mr-2" />
          {t("setting.ai.add-provider")}
        </Button>
      }
    >
      <SettingPanel className="bg-muted/30 px-4 py-3">
        <div className="flex max-w-3xl flex-col gap-2">
          <div className="flex flex-wrap items-center gap-2">
            <span className="rounded-md border border-border bg-background px-2 py-0.5 text-xs font-medium text-foreground">
              {t("setting.ai.byok-label")}
            </span>
            <h4 className="text-sm font-semibold text-foreground">{t("setting.ai.byok-title")}</h4>
          </div>
          <p className="text-sm text-muted-foreground">{t("setting.ai.byok-description")}</p>
          <ul className="space-y-1 text-sm text-muted-foreground">
            {byokNotes.map((note) => (
              <li key={note} className="flex gap-2">
                <span className="mt-2 size-1 rounded-full bg-muted-foreground/60" aria-hidden />
                <span>{t(note)}</span>
              </li>
            ))}
          </ul>
        </div>
      </SettingPanel>

      <SettingGroup title={t("setting.ai.integrations-title")} description={t("setting.ai.integrations-description")}>
        <SettingTable
          columns={[
            {
              key: "title",
              header: t("common.name"),
              render: (_, provider: LocalAIProvider) => (
                <div className="flex flex-col gap-0.5">
                  <span className="text-foreground">{provider.title}</span>
                  <span className="font-mono text-xs text-muted-foreground">{provider.id}</span>
                </div>
              ),
            },
            {
              key: "type",
              header: t("setting.ai.provider-type"),
              render: (_, provider: LocalAIProvider) => <span>{getProviderTypeLabel(provider.type)}</span>,
            },
            {
              key: "endpoint",
              header: t("setting.ai.endpoint"),
              render: (_, provider: LocalAIProvider) => (
                <span className="font-mono text-xs">{provider.endpoint || t("setting.ai.default-endpoint")}</span>
              ),
            },
            {
              key: "apiKeySet",
              header: t("setting.ai.api-key"),
              render: (_, provider: LocalAIProvider) => (
                <span className="font-mono text-xs">{provider.apiKeySet ? provider.apiKeyHint || t("setting.ai.configured") : "-"}</span>
              ),
            },
            {
              key: "actions",
              header: "",
              className: "text-right",
              render: (_, provider: LocalAIProvider) => (
                <DropdownMenu>
                  <DropdownMenuTrigger render={<Button variant="outline" size="sm" />}>
                    <MoreVerticalIcon className="w-4 h-auto" />
                  </DropdownMenuTrigger>
                  <DropdownMenuContent align="end" sideOffset={2}>
                    <DropdownMenuItem onClick={() => handleEditProvider(provider)}>{t("common.edit")}</DropdownMenuItem>
                    <DropdownMenuItem onClick={() => setDeleteTarget(provider)} className="text-destructive focus:text-destructive">
                      {t("common.delete")}
                    </DropdownMenuItem>
                  </DropdownMenuContent>
                </DropdownMenu>
              ),
            },
          ]}
          data={providers}
          emptyMessage={t("setting.ai.no-providers")}
          getRowKey={(provider) => provider.id}
        />
      </SettingGroup>

      <SettingGroup
        title={t("setting.ai.generation-title")}
        description={t("setting.ai.generation-description")}
        showSeparator
        actions={
          <Button disabled={!generationHasChanges} onClick={handleSaveGeneration}>
            {t("common.save")}
          </Button>
        }
      >
        <CapabilityForm providers={providers} draft={generation} onChange={setGeneration} />
        <div className="mt-3 flex flex-wrap items-center gap-2">
          <ReadinessLabel label={t("setting.ai.text-generation")} state={readiness?.textGeneration} />
          <ReadinessLabel label={t("setting.ai.streaming")} state={readiness?.streaming} />
          <ReadinessLabel label={t("setting.ai.structured-tools")} state={readiness?.structuredTools} />
          <Button
            variant="outline"
            size="sm"
            disabled={testingCapability !== undefined || !generation.providerId || !generation.model.trim()}
            onClick={() => handleTestCapability(InstanceSetting_AICapability.TEXT_GENERATION, generation)}
          >
            {testingCapability === InstanceSetting_AICapability.TEXT_GENERATION ? t("setting.ai.testing") : t("setting.ai.test-generation")}
          </Button>
          <Button
            variant="outline"
            size="sm"
            disabled={testingCapability !== undefined || !generation.providerId || !generation.model.trim()}
            onClick={() => handleTestCapability(InstanceSetting_AICapability.STREAMING, generation)}
          >
            {t("setting.ai.test-streaming")}
          </Button>
          <Button
            variant="outline"
            size="sm"
            disabled={testingCapability !== undefined || !generation.providerId || !generation.model.trim()}
            onClick={() => handleTestCapability(InstanceSetting_AICapability.STRUCTURED_TOOLS, generation)}
          >
            {t("setting.ai.test-tools")}
          </Button>
        </div>
      </SettingGroup>

      <SettingGroup
        title={t("setting.ai.embedding-title")}
        description={t("setting.ai.embedding-description")}
        showSeparator
        actions={
          <Button disabled={!embeddingHasChanges} onClick={handleSaveEmbedding}>
            {t("common.save")}
          </Button>
        }
      >
        <CapabilityForm providers={providers} draft={embedding} onChange={setEmbedding} embedding />
        <div className="mt-3 flex max-w-3xl items-start gap-3 rounded-md border border-border p-3">
          <Switch checked={externalProcessingAcknowledged} onCheckedChange={setExternalProcessingAcknowledged} />
          <div>
            <Label>{t("setting.ai.embedding-disclosure-title")}</Label>
            <p className="text-xs text-muted-foreground">{t("setting.ai.embedding-disclosure-description")}</p>
          </div>
        </div>
        <div className="mt-3 flex flex-wrap items-center gap-2">
          <ReadinessLabel label={t("setting.ai.embeddings")} state={readiness?.embeddings} />
          <Button
            variant="outline"
            size="sm"
            disabled={
              testingCapability !== undefined || !embedding.providerId || !embedding.model.trim() || !externalProcessingAcknowledged
            }
            onClick={() => handleTestCapability(InstanceSetting_AICapability.EMBEDDINGS, embedding)}
          >
            {testingCapability === InstanceSetting_AICapability.EMBEDDINGS ? t("setting.ai.testing") : t("setting.ai.test-embedding")}
          </Button>
        </div>
      </SettingGroup>

      <SettingGroup
        title={t("setting.ai.transcription-title")}
        description={t("setting.ai.transcription-description")}
        showSeparator
        actions={
          <Button disabled={!transcriptionHasChanges} onClick={handleSaveTranscription}>
            {t("common.save")}
          </Button>
        }
      >
        <TranscriptionForm
          providers={providers}
          transcription={transcription}
          onChange={setTranscription}
          referencedProvider={transcriptionProviderRef}
        />
      </SettingGroup>

      <AIProviderDialog
        provider={editingProvider}
        onOpenChange={(open) => !open && setEditingProvider(undefined)}
        onSave={handleSaveProvider}
      />

      <ConfirmDialog
        open={!!deleteTarget}
        onOpenChange={(open) => !open && setDeleteTarget(undefined)}
        title={deleteTarget ? t("setting.ai.delete-provider", { title: deleteTarget.title }) : ""}
        confirmLabel={t("common.delete")}
        cancelLabel={t("common.cancel")}
        onConfirm={handleDeleteProvider}
        confirmVariant="destructive"
      />
    </SettingSection>
  );
};

interface CapabilityFormProps {
  providers: LocalAIProvider[];
  draft: LocalCapability;
  onChange: (next: LocalCapability) => void;
  embedding?: boolean;
}

const CapabilityForm = ({ providers, draft, onChange, embedding = false }: CapabilityFormProps) => {
  const t = useTranslate();
  const providerOptions = useMemo(
    () => [
      { value: "__none__", label: t("setting.ai.capability-no-provider") },
      ...providers.map((provider) => ({ value: provider.id, label: provider.title || provider.id })),
    ],
    [providers, t],
  );
  const update = (partial: Partial<LocalCapability>) => onChange({ ...draft, ...partial });

  return (
    <div className="grid max-w-3xl grid-cols-1 gap-3 sm:grid-cols-2">
      <div className="flex flex-col gap-1.5">
        <Label>{t("setting.ai.capability-provider")}</Label>
        <Select
          value={draft.providerId || "__none__"}
          items={providerOptions}
          onValueChange={(value) => update({ providerId: value === "__none__" ? "" : value })}
          disabled={providers.length === 0}
        >
          <SelectTrigger className="w-full">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {providerOptions.map((option) => (
              <SelectItem key={option.value} value={option.value}>
                {option.label}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </div>
      <div className="flex flex-col gap-1.5">
        <Label>{t("setting.ai.capability-model")}</Label>
        <Input
          value={draft.model}
          onChange={(event) => update({ model: event.target.value })}
          disabled={!draft.providerId}
          maxLength={256}
        />
      </div>
      {embedding && (
        <div className="flex flex-col gap-1.5">
          <Label>{t("setting.ai.embedding-dimensions")}</Label>
          <Input
            type="number"
            min={0}
            max={65536}
            value={draft.dimensions || ""}
            onChange={(event) => update({ dimensions: Number(event.target.value) || 0 })}
            disabled={!draft.providerId}
            placeholder={t("setting.ai.embedding-dimensions-placeholder")}
          />
        </div>
      )}
    </div>
  );
};

const ReadinessLabel = ({ label, state }: { label: string; state: InstanceSetting_CapabilityState | undefined }) => {
  const t = useTranslate();
  const value = state ?? InstanceSetting_CapabilityState.NOT_CONFIGURED;
  const ready = value === InstanceSetting_CapabilityState.READY;
  const readinessLabel = {
    [InstanceSetting_CapabilityState.CAPABILITY_STATE_UNSPECIFIED]: t("setting.ai.readiness-capability-state-unspecified"),
    [InstanceSetting_CapabilityState.NOT_CONFIGURED]: t("setting.ai.readiness-not-configured"),
    [InstanceSetting_CapabilityState.UNVALIDATED]: t("setting.ai.readiness-unvalidated"),
    [InstanceSetting_CapabilityState.READY]: t("setting.ai.readiness-ready"),
    [InstanceSetting_CapabilityState.UNAVAILABLE]: t("setting.ai.readiness-unavailable"),
  }[value];
  return (
    <span
      className={`rounded-md border px-2 py-1 text-xs ${ready ? "border-green-500/40 text-green-700" : "border-border text-muted-foreground"}`}
    >
      {label}: {readinessLabel}
    </span>
  );
};

interface TranscriptionFormProps {
  providers: LocalAIProvider[];
  transcription: LocalTranscription;
  referencedProvider: LocalAIProvider | undefined;
  onChange: (next: LocalTranscription) => void;
}

const TranscriptionForm = ({ providers, transcription, referencedProvider, onChange }: TranscriptionFormProps) => {
  const t = useTranslate();
  const noProviders = providers.length === 0;

  const providerOptions = useMemo(
    () => [
      { value: "__none__", label: t("setting.ai.transcription-no-provider") },
      ...providers.map((provider) => ({ value: provider.id, label: provider.title || provider.id })),
    ],
    [providers, t],
  );

  const update = (partial: Partial<LocalTranscription>) => {
    onChange({ ...transcription, ...partial });
  };

  const placeholderForProvider = (provider: LocalAIProvider | undefined) => {
    if (!provider) return "";
    return provider.type === InstanceSetting_AIProviderType.GEMINI
      ? t("setting.ai.transcription-model-placeholder-gemini")
      : t("setting.ai.transcription-model-placeholder-openai");
  };

  return (
    <div className="grid grid-cols-1 sm:grid-cols-2 gap-3 max-w-3xl">
      <div className="flex flex-col gap-1.5 sm:col-span-2">
        <Label>{t("setting.ai.transcription-provider")}</Label>
        <Select
          value={transcription.providerId || "__none__"}
          items={providerOptions}
          onValueChange={(value) => update({ providerId: value === "__none__" ? "" : value })}
          disabled={noProviders}
        >
          <SelectTrigger className="w-full">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {providerOptions.map((option) => (
              <SelectItem key={option.value} value={option.value}>
                {option.label}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        {noProviders && <p className="text-xs text-muted-foreground">{t("setting.ai.transcription-empty-providers")}</p>}
        {referencedProvider && !referencedProvider.apiKeySet && (
          <p className="text-xs text-destructive">{t("setting.ai.transcription-warning-no-key")}</p>
        )}
      </div>

      <div className="flex flex-col gap-1.5 sm:col-span-2">
        <Label>{t("setting.ai.transcription-model")}</Label>
        <Input
          value={transcription.model}
          onChange={(e) => update({ model: e.target.value })}
          placeholder={placeholderForProvider(referencedProvider)}
          disabled={!transcription.providerId}
          maxLength={256}
        />
        <p className="text-xs text-muted-foreground">{t("setting.ai.transcription-model-help")}</p>
      </div>

      <div className="flex flex-col gap-1.5">
        <Label>{t("setting.ai.transcription-language")}</Label>
        <Input
          value={transcription.language}
          onChange={(e) => update({ language: e.target.value })}
          placeholder={t("setting.ai.transcription-language-placeholder")}
          disabled={!transcription.providerId}
          maxLength={32}
        />
        <p className="text-xs text-muted-foreground">{t("setting.ai.transcription-language-help")}</p>
      </div>

      <div className="flex flex-col gap-1.5 sm:col-span-2">
        <Label>{t("setting.ai.transcription-prompt")}</Label>
        <Textarea
          value={transcription.prompt}
          onChange={(e) => update({ prompt: e.target.value })}
          placeholder={t("setting.ai.transcription-prompt-placeholder")}
          rows={3}
          disabled={!transcription.providerId}
          maxLength={4096}
        />
        <p className="text-xs text-muted-foreground">{t("setting.ai.transcription-prompt-help")}</p>
      </div>
    </div>
  );
};

interface AIProviderDialogProps {
  provider?: LocalAIProvider;
  onOpenChange: (open: boolean) => void;
  onSave: (provider: LocalAIProvider) => void;
}

const AIProviderDialog = ({ provider, onOpenChange, onSave }: AIProviderDialogProps) => {
  const t = useTranslate();
  const [draft, setDraft] = useState<LocalAIProvider>(() => provider ?? newProvider());

  useEffect(() => {
    const next = provider ?? newProvider();
    setDraft(next);
  }, [provider]);

  const updateDraft = (partial: Partial<LocalAIProvider>) => {
    setDraft((prev) => ({ ...prev, ...partial }));
  };

  const handleSave = () => {
    onSave(draft);
  };

  return (
    <Dialog open={!!provider} onOpenChange={onOpenChange}>
      <DialogContent size="2xl">
        <DialogHeader>
          <DialogTitle>{provider?.apiKeySet ? t("setting.ai.edit-provider") : t("setting.ai.add-provider")}</DialogTitle>
          <DialogDescription>{t("setting.ai.dialog-description")}</DialogDescription>
        </DialogHeader>

        <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
          <div className="flex flex-col gap-1.5">
            <Label>{t("setting.ai.provider-title")}</Label>
            <Input value={draft.title} onChange={(e) => updateDraft({ title: e.target.value })} placeholder="OpenAI" />
          </div>

          <div className="flex flex-col gap-1.5">
            <Label>{t("setting.ai.provider-type")}</Label>
            <Select
              value={String(draft.type)}
              items={providerTypeSelectOptions}
              onValueChange={(value) => updateDraft({ type: Number(value) as InstanceSetting_AIProviderType })}
            >
              <SelectTrigger className="w-full">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {providerTypeSelectOptions.map((option) => (
                  <SelectItem key={option.value} value={option.value}>
                    {option.label}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>

          <div className="flex flex-col gap-1.5 sm:col-span-2">
            <Label>{t("setting.ai.endpoint")}</Label>
            <Input
              value={draft.endpoint}
              onChange={(e) => updateDraft({ endpoint: e.target.value })}
              placeholder={getDefaultEndpointPlaceholder(draft.type)}
            />
            <p className="text-xs text-muted-foreground">{t("setting.ai.endpoint-hint")}</p>
          </div>

          <div className="flex flex-col gap-1.5 sm:col-span-2">
            <Label>{t("setting.ai.api-key")}</Label>
            <Input
              type="password"
              value={draft.apiKey}
              onChange={(e) => updateDraft({ apiKey: e.target.value })}
              placeholder={draft.apiKeySet ? t("setting.ai.keep-api-key") : ""}
            />
            {draft.apiKeySet && (
              <p className="text-xs text-muted-foreground">{t("setting.ai.current-key", { key: draft.apiKeyHint || "-" })}</p>
            )}
          </div>

          <div className="flex items-start gap-3 sm:col-span-2 rounded-md border border-border p-3">
            <Switch checked={draft.allowPrivateNetwork} onCheckedChange={(allowPrivateNetwork) => updateDraft({ allowPrivateNetwork })} />
            <div>
              <Label>{t("setting.ai.allow-private-network")}</Label>
              <p className="text-xs text-muted-foreground">{t("setting.ai.allow-private-network-description")}</p>
            </div>
          </div>
        </div>

        <DialogFooter>
          <Button variant="ghost" onClick={() => onOpenChange(false)}>
            {t("common.cancel")}
          </Button>
          <Button onClick={handleSave}>{t("common.save")}</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
};

const getDefaultEndpointPlaceholder = (type: InstanceSetting_AIProviderType) => {
  switch (type) {
    case InstanceSetting_AIProviderType.OPENAI:
      return "https://api.openai.com/v1";
    case InstanceSetting_AIProviderType.GEMINI:
      return "https://generativelanguage.googleapis.com/v1beta";
    default:
      return "";
  }
};

export default AISection;
