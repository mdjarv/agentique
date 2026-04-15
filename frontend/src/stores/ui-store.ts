import { create } from "zustand";
import { createJSONStorage, persist } from "zustand/middleware";
import type { EffortLevel } from "~/lib/composer-constants";
import type { ModelId } from "~/lib/session/actions";
import type { AutoApproveMode } from "~/stores/chat-store";

export type Theme = "light" | "dark" | "system";

const LEGACY_COLLAPSED_KEY = "agentique:collapsed-projects";

function readLegacyCollapsedProjects(): string[] {
  try {
    const raw = localStorage.getItem(LEGACY_COLLAPSED_KEY);
    if (raw) return JSON.parse(raw) as string[];
  } catch {}
  return [];
}

export interface SessionDefaults {
  worktree: boolean;
  planMode: boolean;
  autoApproveMode: AutoApproveMode;
  model: ModelId;
  effort: EffortLevel;
}

export const DEFAULT_SESSION_DEFAULTS: SessionDefaults = {
  worktree: true,
  planMode: false,
  autoApproveMode: "fullAuto",
  model: "opus[1m]",
  effort: "",
};

interface UIState {
  drafts: Record<string, string>;
  stashes: Record<string, string>;
  collapsedProjectIds: string[];
  rightPanelCollapsed: boolean;
  browserPanelWidth: number;
  teamPanelOpen: boolean;
  theme: Theme;

  setDraft: (sessionId: string, text: string) => void;
  clearDraft: (sessionId: string) => void;
  setStash: (sessionId: string, text: string) => void;
  clearStash: (sessionId: string) => void;
  setProjectCollapsed: (projectId: string, collapsed: boolean) => void;
  setRightPanelCollapsed: (collapsed: boolean) => void;
  setBrowserPanelWidth: (width: number) => void;
  setTeamPanelOpen: (open: boolean) => void;
  setTheme: (theme: Theme) => void;
}

export const useUIStore = create<UIState>()(
  persist(
    (set) => ({
      drafts: {},
      stashes: {},
      collapsedProjectIds: readLegacyCollapsedProjects(),
      rightPanelCollapsed: true,
      browserPanelWidth: 500,
      teamPanelOpen: false,
      theme: "dark" as Theme,

      setDraft: (sessionId, text) =>
        set((s) => {
          if (!text) {
            const { [sessionId]: _, ...rest } = s.drafts;
            return { drafts: rest };
          }
          return { drafts: { ...s.drafts, [sessionId]: text } };
        }),

      clearDraft: (sessionId) =>
        set((s) => {
          if (!(sessionId in s.drafts)) return s;
          const { [sessionId]: _, ...rest } = s.drafts;
          return { drafts: rest };
        }),

      setStash: (sessionId, text) =>
        set((s) => {
          if (!text) {
            const { [sessionId]: _, ...rest } = s.stashes;
            return { stashes: rest };
          }
          return { stashes: { ...s.stashes, [sessionId]: text } };
        }),

      clearStash: (sessionId) =>
        set((s) => {
          if (!(sessionId in s.stashes)) return s;
          const { [sessionId]: _, ...rest } = s.stashes;
          return { stashes: rest };
        }),

      setProjectCollapsed: (projectId, collapsed) =>
        set((s) => {
          const has = s.collapsedProjectIds.includes(projectId);
          if (collapsed && !has) {
            return { collapsedProjectIds: [...s.collapsedProjectIds, projectId] };
          }
          if (!collapsed && has) {
            return { collapsedProjectIds: s.collapsedProjectIds.filter((id) => id !== projectId) };
          }
          return s;
        }),

      setRightPanelCollapsed: (collapsed) => set({ rightPanelCollapsed: collapsed }),

      setBrowserPanelWidth: (width) =>
        set({ browserPanelWidth: Math.max(300, Math.min(900, width)) }),

      setTeamPanelOpen: (open) => set({ teamPanelOpen: open }),

      setTheme: (theme) => set({ theme }),
    }),
    {
      name: "agentique:ui",
      storage: createJSONStorage(() => localStorage),
      partialize: (state) => ({
        drafts: state.drafts,
        stashes: state.stashes,
        collapsedProjectIds: state.collapsedProjectIds,
        rightPanelCollapsed: state.rightPanelCollapsed,
        browserPanelWidth: state.browserPanelWidth,
        teamPanelOpen: state.teamPanelOpen,
        theme: state.theme,
      }),
      onRehydrateStorage: () => () => {
        try {
          localStorage.removeItem(LEGACY_COLLAPSED_KEY);
        } catch {}
      },
    },
  ),
);
