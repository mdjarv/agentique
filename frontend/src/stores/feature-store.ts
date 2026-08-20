import { create } from "zustand";

interface Features {
  browser: boolean;
  teams: boolean;
}

interface FeatureState {
  features: Features;
  /** Identity of the primary machine (the server serving this SPA). */
  machineId: string;
  machineLabel: string;
  loaded: boolean;
  load: () => Promise<void>;
}

const DEFAULT_FEATURES: Features = { browser: false, teams: false };

export const useFeatureStore = create<FeatureState>((set, get) => ({
  features: DEFAULT_FEATURES,
  machineId: "",
  machineLabel: "",
  loaded: false,
  load: async () => {
    if (get().loaded) return;
    try {
      const resp = await fetch("/api/health");
      if (!resp.ok) return;
      const data = await resp.json();
      if (data.features) {
        set({
          features: { ...DEFAULT_FEATURES, ...data.features },
          machineId: data.machineId ?? "",
          machineLabel: data.machineLabel ?? "",
          loaded: true,
        });
      }
    } catch {
      // Silently fail — features stay at defaults.
    }
  },
}));
