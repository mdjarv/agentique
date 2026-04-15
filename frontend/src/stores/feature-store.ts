import { create } from "zustand";

interface Features {
  browser: boolean;
  teams: boolean;
}

interface FeatureState {
  features: Features;
  loaded: boolean;
  load: () => Promise<void>;
}

const DEFAULT_FEATURES: Features = { browser: false, teams: false };

export const useFeatureStore = create<FeatureState>((set, get) => ({
  features: DEFAULT_FEATURES,
  loaded: false,
  load: async () => {
    if (get().loaded) return;
    try {
      const resp = await fetch("/api/health");
      if (!resp.ok) return;
      const data = await resp.json();
      if (data.features) {
        set({ features: { ...DEFAULT_FEATURES, ...data.features }, loaded: true });
      }
    } catch {
      // Silently fail — features stay at defaults.
    }
  },
}));
