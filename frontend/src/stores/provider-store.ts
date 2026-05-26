import { create } from "zustand";
import type { ProviderModelInfo, ProviderModels } from "~/lib/generated-types";
import type { ProviderId } from "~/lib/session/actions";

interface ProviderStore {
  /** Per-provider catalog. Missing key = not yet loaded. */
  models: Record<string, ProviderModelInfo[]>;
  /** Per-provider source flag (cache/static/fallback). */
  sources: Record<string, string>;
  setProviders: (providers: ProviderModels[]) => void;
}

export const useProviderStore = create<ProviderStore>((set) => ({
  models: {},
  sources: {},
  setProviders: (providers) =>
    set(() => {
      const models: Record<string, ProviderModelInfo[]> = {};
      const sources: Record<string, string> = {};
      for (const p of providers) {
        models[p.provider] = p.models;
        sources[p.provider] = p.source;
      }
      return { models, sources };
    }),
}));

export function modelsForProvider(provider: ProviderId | undefined): ProviderModelInfo[] {
  const state = useProviderStore.getState();
  if (!provider) {
    return Object.values(state.models).flat();
  }
  return state.models[provider] ?? [];
}
