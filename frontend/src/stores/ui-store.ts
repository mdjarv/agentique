import { create } from "zustand";
import { createJSONStorage, persist } from "zustand/middleware";
import type { EffortLevel } from "~/lib/composer-constants";
import type { ModelId } from "~/lib/session/actions";
import type { AutoApproveMode } from "~/stores/chat-store";

export type Theme = "light" | "dark" | "system";

/** Which view the shared collapsible right panel shows. */
export type RightPanelView = "browser" | "workflow";

const LEGACY_COLLAPSED_KEY = "agentique:collapsed-projects";

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
  effort: "xhigh",
};

interface UIState {
  drafts: Record<string, string>;
  stashes: Record<string, string[]>;
  /** Pre-migration cache of locally-pinned project IDs.
   *  Drained once on startup and pushed to the server (see usePinnedMigration). */
  legacyPinnedProjectIds: string[];
  rightPanelCollapsed: boolean;
  /** Which content the shared right panel shows when expanded. */
  rightPanelView: RightPanelView;
  todoSidebarCollapsed: boolean;
  browserPanelWidth: number;
  theme: Theme;

  setDraft: (sessionId: string, text: string) => void;
  clearDraft: (sessionId: string) => void;
  pushStash: (sessionId: string, text: string) => void;
  popStash: (sessionId: string) => string | undefined;
  clearStash: (sessionId: string) => void;
  clearLegacyPinnedProjectIds: () => void;
  setRightPanelCollapsed: (collapsed: boolean) => void;
  setRightPanelView: (view: RightPanelView) => void;
  setTodoSidebarCollapsed: (collapsed: boolean) => void;
  setBrowserPanelWidth: (width: number) => void;
  setTheme: (theme: Theme) => void;
}

export const useUIStore = create<UIState>()(
  persist(
    (set, get) => ({
      drafts: {},
      stashes: {},
      legacyPinnedProjectIds: [],
      rightPanelCollapsed: true,
      rightPanelView: "browser" as RightPanelView,
      todoSidebarCollapsed: false,
      browserPanelWidth: 500,
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

      clearLegacyPinnedProjectIds: () => set({ legacyPinnedProjectIds: [] }),

      setRightPanelCollapsed: (collapsed) => set({ rightPanelCollapsed: collapsed }),
      setRightPanelView: (view) => set({ rightPanelView: view }),

      setTodoSidebarCollapsed: (collapsed) => set({ todoSidebarCollapsed: collapsed }),

      setBrowserPanelWidth: (width) =>
        set({ browserPanelWidth: Math.max(300, Math.min(900, width)) }),

      setTheme: (theme) => set({ theme }),
    }),
    {
      name: "agentique:ui",
      version: 5,
      storage: createJSONStorage(() => localStorage),
      migrate: (persisted, version) => {
        const state = persisted as Record<string, unknown>;
        if (version === 0 && state.stashes) {
          const old = state.stashes as Record<string, string | string[]>;
          const migrated: Record<string, string[]> = {};
          for (const [k, v] of Object.entries(old)) {
            migrated[k] = typeof v === "string" ? [v] : v;
          }
          state.stashes = migrated;
        }
        if (version < 2) {
          const ids = (state.collapsedProjectIds as string[] | undefined) ?? [];
          state.expandedProjects = Object.fromEntries(ids.map((id) => [id, false]));
          state.expandedFolders = {};
          state.sidebarFocusMode = false;
          delete state.collapsedProjectIds;
        }
        if (version < 3) {
          // Seed pinned from explicitly-expanded projects (previous focus-mode proxy).
          const expanded = (state.expandedProjects as Record<string, boolean>) ?? {};
          state.pinnedProjectIds = Object.entries(expanded)
            .filter(([, v]) => v === true)
            .map(([id]) => id);
        }
        if (version < 4) {
          // Pinning moved server-side; hold the local IDs aside for one-time
          // migration in usePinnedMigration, then clear the field.
          state.legacyPinnedProjectIds = (state.pinnedProjectIds as string[] | undefined) ?? [];
          delete state.pinnedProjectIds;
        }
        if (version < 5) {
          // The folder sidebar (and its focus mode) was replaced by the flat
          // thread sidebar; its per-folder/per-project expand state is gone.
          delete state.expandedProjects;
          delete state.expandedFolders;
          delete state.sidebarFocusMode;
        }
        return state;
      },
      partialize: (state) => ({
        drafts: state.drafts,
        stashes: state.stashes,
        legacyPinnedProjectIds: state.legacyPinnedProjectIds,
        rightPanelCollapsed: state.rightPanelCollapsed,
        rightPanelView: state.rightPanelView,
        todoSidebarCollapsed: state.todoSidebarCollapsed,
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
