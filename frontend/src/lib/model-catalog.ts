import { useMemo } from "react";
import { useProviderStore } from "~/stores/provider-store";

export const PROVIDERS = ["claude", "codex"] as const;
export type ProviderId = (typeof PROVIDERS)[number];

export const PROVIDER_LABELS: Record<ProviderId, string> = {
  claude: "Claude",
  codex: "Codex",
};

/**
 * A model slug passed to the provider CLI. Deliberately a bare string, not a
 * union: the backend catalog is the source of truth and gains new models at
 * runtime (see backend/internal/providers), so a closed union here would mean a
 * frontend release for every upstream model launch.
 */
export type ModelId = string;

export interface ModelOption {
  value: ModelId;
  label: string;
  group: string;
  description?: string;
}

/**
 * Shown only in the window between app start and the first providers.models
 * response. Labels are family names without versions on purpose — a version
 * baked in here is a version that goes stale.
 */
const FALLBACK_MODELS: Record<ProviderId, { slug: string; label: string }[]> = {
  claude: [
    { slug: "haiku", label: "Haiku" },
    { slug: "sonnet", label: "Sonnet" },
    { slug: "opus", label: "Opus" },
    { slug: "fable", label: "Fable" },
    { slug: "sonnet[1m]", label: "Sonnet (1M)" },
    { slug: "opus[1m]", label: "Opus (1M)" },
  ],
  codex: [
    { slug: "gpt-5", label: "GPT-5" },
    { slug: "gpt-5-codex", label: "GPT-5 Codex" },
    { slug: "gpt-5-mini", label: "GPT-5 Mini" },
  ],
};

export const DEFAULT_MODEL_FOR_PROVIDER: Record<ProviderId, ModelId> = {
  claude: "sonnet",
  codex: "gpt-5",
};

type Catalog = Record<string, { slug: string; displayName: string; description?: string }[]>;

/**
 * Flattens the backend catalog into picker options, grouped by provider and
 * falling back per provider (not globally) so a codex cache miss never hides
 * claude's learned labels.
 */
export function buildModelOptions(
  catalog: Catalog,
  filterProvider?: ProviderId,
): { options: ModelOption[]; providerOf: (slug: string) => ProviderId | undefined } {
  const providers = filterProvider ? [filterProvider] : PROVIDERS;
  const owner = new Map<string, ProviderId>();
  const options: ModelOption[] = [];

  for (const p of providers) {
    const group = PROVIDER_LABELS[p];
    const dynamic = catalog[p];
    const entries =
      dynamic && dynamic.length > 0
        ? dynamic.map((m) => ({
            value: m.slug,
            label: m.displayName || m.slug,
            description: m.description,
          }))
        : FALLBACK_MODELS[p].map((m) => ({
            value: m.slug,
            label: m.label,
            description: undefined as string | undefined,
          }));

    for (const entry of entries) {
      owner.set(entry.value, p);
      options.push({
        value: entry.value,
        label: entry.label,
        group,
        ...(entry.description ? { description: entry.description } : {}),
      });
    }
  }

  return { options, providerOf: (slug) => owner.get(slug) };
}

/**
 * Reactive picker options. `s.models` is a stable store reference, so the memo
 * only recomputes when the catalog actually changes.
 */
export function useModelOptions(filterProvider?: ProviderId) {
  const catalog = useProviderStore((s) => s.models);
  return useMemo(() => buildModelOptions(catalog, filterProvider), [catalog, filterProvider]);
}

/**
 * Non-reactive label lookup for one-off rendering. Falls back to the static
 * label and finally to the slug itself, so an unknown model still renders.
 */
export function modelLabel(slug: ModelId | undefined): string {
  if (!slug) return "";
  const catalog = useProviderStore.getState().models;
  for (const models of Object.values(catalog)) {
    const hit = models.find((m) => m.slug === slug);
    if (hit) return hit.displayName || hit.slug;
  }
  for (const p of PROVIDERS) {
    const hit = FALLBACK_MODELS[p].find((m) => m.slug === slug);
    if (hit) return hit.label;
  }
  return slug;
}

/**
 * Resolves which provider owns a model slug, preferring the live catalog so a
 * newly-advertised model routes correctly without a frontend change.
 */
export function providerForModel(model: ModelId | undefined): ProviderId {
  if (!model) return "claude";
  const catalog = useProviderStore.getState().models;
  for (const p of PROVIDERS) {
    if (catalog[p]?.some((m) => m.slug === model)) return p;
  }
  for (const p of PROVIDERS) {
    if (FALLBACK_MODELS[p].some((m) => m.slug === model)) return p;
  }
  return "claude";
}
