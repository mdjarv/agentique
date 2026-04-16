import { create } from "zustand";
import { createJSONStorage, persist } from "zustand/middleware";
import type { EffortLevel } from "~/lib/composer-constants";
import type { ModelId } from "~/lib/session/actions";
import type { AutoApproveMode } from "~/stores/chat-store";

export type Theme = "light" | "dark" | "system";
export type SidebarTab = "sessions" | "teams";

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
  stashes: Record<string, string[]>;
  collapsedProjectIds: string[];
  rightPanelCollapsed: boolean;
  browserPanelWidth: number;
  /** NOT persisted — defaults to "sessions" on load. */
  sidebarTab: SidebarTab;
  theme: Theme;

  setDraft: (sessionId: string, text: string) => void;
  clearDraft: (sessionId: string) => void;
  pushStash: (sessionId: string, text: string) => void;
  popStash: (sessionId: string) => string | undefined;
  clearStash: (sessionId: string) => void;
  setProjectCollapsed: (projectId: string, collapsed: boolean) => void;
  setRightPanelCollapsed: (collapsed: boolean) => void;
  setBrowserPanelWidth: (width: number) => void;
  setSidebarTab: (tab: SidebarTab) => void;
  setTheme: (theme: Theme) => void;
}

export const useUIStore = create<UIState>()(
  persist(
    (set, get) => ({
      drafts: {},
      stashes: {},
      collapsedProjectIds: readLegacyCollapsedProjects(),
      rightPanelCollapsed: true,
      browserPanelWidth: 500,
      sidebarTab: "sessions" as SidebarTab,
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

      pushStash: (sessionId: string, text: string) =>
        set((s) => {
          const trimmed = text.trim();
          if (!trimmed) return s;
          const stack = s.stashes[sessionId] ?? [];
          return { stashes: { ...s.stashes, [sessionId]: [...stack, trimmed] } };
        }),

      popStash: (sessionId: string) => {
        const stack = get().stashes[sessionId];
        if (!stack?.length) return undefined;
        const popped = stack[stack.length - 1];
        const rest = stack.slice(0, -1);
        if (rest.length === 0) {
          const { [sessionId]: _, ...others } = get().stashes;
          set({ stashes: others });
        } else {
          set({ stashes: { ...get().stashes, [sessionId]: rest } });
        }
        return popped;
      },

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

      setSidebarTab: (tab) => set({ sidebarTab: tab }),

      setTheme: (theme) => set({ theme }),
    }),
    {
      name: "agentique:ui",
      version: 1,
      storage: createJSONStorage(() => localStorage),
      migrate: (persisted, version) => {
        const state = persisted as Record<string, unknown>;
        if (version === 0 && state.stashes) {
          // Migrate stashes from Record<string, string> → Record<string, string[]>
          const old = state.stashes as Record<string, string | string[]>;
          const migrated: Record<string, string[]> = {};
          for (const [k, v] of Object.entries(old)) {
            migrated[k] = typeof v === "string" ? [v] : v;
          }
          state.stashes = migrated;
        }
        return state;
      },
      partialize: (state) => ({
        drafts: state.drafts,
        stashes: state.stashes,
        collapsedProjectIds: state.collapsedProjectIds,
        rightPanelCollapsed: state.rightPanelCollapsed,
        browserPanelWidth: state.browserPanelWidth,
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
