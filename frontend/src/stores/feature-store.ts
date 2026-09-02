import { create } from "zustand";

interface Features {
  browser: boolean;
  teams: boolean;
  /** Live spoken dialog. Off unless [experimental] voice is set. */
  voice: boolean;
  /**
   * Persistent agent memory. Off unless [brain] enabled is set, which is the
   * default — so the Brain destination is absent, not empty. When it is off the
   * server mounts no /api/brain routes at all, so any surface reading the brain
   * has to check this first rather than render and fail.
   */
  brain: boolean;
}

interface FeatureState {
  features: Features;
  /** Identity of the primary machine (the server serving this SPA). */
  machineId: string;
  machineLabel: string;
  /** Icon id (lucide) this host shows for itself; empty = the default glyph. */
  machineIcon: string;
  /** AGENTIQUE_MACHINE_LABEL is set, so the name can't be changed from here. */
  machineLabelPinned: boolean;
  /** This host's own OS (GOOS), from /api/health. Empty on older servers. */
  machinePlatformOs: string;
  version: string;
  loaded: boolean;
  load: () => Promise<void>;
  /** Rename / re-face this host. Presentation is local and restart-free. */
  saveHostPresentation: (label: string, icon: string) => Promise<void>;
}

const DEFAULT_FEATURES: Features = { browser: false, teams: false, voice: false, brain: false };

export const useFeatureStore = create<FeatureState>((set, get) => ({
  features: DEFAULT_FEATURES,
  machineId: "",
  machineLabel: "",
  machineIcon: "",
  machineLabelPinned: false,
  machinePlatformOs: "",
  version: "",
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
          machineIcon: data.machineIcon ?? "",
          machineLabelPinned: !!data.machineLabelPinned,
          machinePlatformOs: data.platform?.os ?? "",
          version: data.version ?? "",
          loaded: true,
        });
      }
    } catch {
      // Silently fail — features stay at defaults.
    }
  },
  saveHostPresentation: async (label, icon) => {
    const resp = await fetch("/api/machine/presentation", {
      method: "PUT",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ label, icon }),
    });
    if (!resp.ok) throw new Error(`save failed (${resp.status})`);
    // The server answers with the *effective* name — a pinned env label wins
    // over the write, so trusting the response keeps the UI honest.
    const data = await resp.json();
    set({
      machineLabel: data.machineLabel ?? label,
      machineIcon: data.machineIcon ?? icon,
      machineLabelPinned: !!data.machineLabelPinned,
    });
  },
}));
