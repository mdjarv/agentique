import { create } from "zustand";
import { createJSONStorage, persist } from "zustand/middleware";
import type { EffortLevel } from "~/lib/composer-constants";
import type { ModelId } from "~/lib/session/actions";
import type { DockView } from "~/lib/session/dock";
import type { AutoApproveMode } from "~/stores/chat-store";

export type Theme = "light" | "dark" | "system";

const LEGACY_COLLAPSED_KEY = "agentique:collapsed-projects";

/** What one session remembers about its dock. */
export interface SessionDockState {
  open: boolean;
  view: DockView;
}

/**
 * The dock is per session, because "what I had open beside this work" is a
 * property of the work. That makes the map unbounded, so it is pruned in
 * insertion order — the oldest sessions to touch their dock lose the memory
 * first, which is the least surprising thing to forget.
 */
const MAX_DOCK_SESSIONS = 120;

function pruneDock(dock: Record<string, SessionDockState>): Record<string, SessionDockState> {
  const keys = Object.keys(dock);
  if (keys.length <= MAX_DOCK_SESSIONS) return dock;
  const keep = keys.slice(keys.length - MAX_DOCK_SESSIONS);
  return Object.fromEntries(keep.map((key) => [key, dock[key] as SessionDockState]));
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
  effort: "xhigh",
};

interface UIState {
  drafts: Record<string, string>;
  stashes: Record<string, string[]>;
  /** Pre-migration cache of locally-pinned project IDs.
   *  Drained once on startup and pushed to the server (see usePinnedMigration). */
  legacyPinnedProjectIds: string[];
  /** Per-session dock state, keyed by session id. Absent means "never opened". */
  dock: Record<string, SessionDockState>;
  /**
   * Dock width and maximization are viewport preferences, not session state:
   * how wide you like a side panel does not change with what is in it.
   */
  dockWidth: number;
  dockMaximized: boolean;
  /** Sync dock: expansion is a preference, not a gesture — it is remembered. */
  syncDockExpanded: boolean;
  theme: Theme;

  setDraft: (sessionId: string, text: string) => void;
  clearDraft: (sessionId: string) => void;
  pushStash: (sessionId: string, text: string) => void;
  popStash: (sessionId: string) => string | undefined;
  clearStash: (sessionId: string) => void;
  clearLegacyPinnedProjectIds: () => void;
  /** Open the dock on a view. Use for any gesture that means "show me this". */
  openDock: (sessionId: string, view: DockView) => void;
  setDockOpen: (sessionId: string, open: boolean) => void;
  setDockView: (sessionId: string, view: DockView) => void;
  setDockWidth: (width: number) => void;
  setDockMaximized: (maximized: boolean) => void;
  setSyncDockExpanded: (expanded: boolean) => void;
  setTheme: (theme: Theme) => void;
}

const DEFAULT_DOCK: SessionDockState = { open: false, view: "work" };

export function sessionDock(
  state: Pick<UIState, "dock">,
  sessionId: string | null,
): SessionDockState {
  if (!sessionId) return DEFAULT_DOCK;
  return state.dock[sessionId] ?? DEFAULT_DOCK;
}

export const useUIStore = create<UIState>()(
  persist(
    (set, get) => ({
      drafts: {},
      stashes: {},
      legacyPinnedProjectIds: [],
      dock: {},
      dockWidth: 500,
      dockMaximized: false,
      syncDockExpanded: false,
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

      openDock: (sessionId, view) =>
        set((s) => ({
          dock: pruneDock({ ...s.dock, [sessionId]: { open: true, view } }),
        })),

      setDockOpen: (sessionId, open) =>
        set((s) => {
          const current = s.dock[sessionId] ?? DEFAULT_DOCK;
          if (current.open === open) return s;
          return { dock: pruneDock({ ...s.dock, [sessionId]: { ...current, open } }) };
        }),

      setDockView: (sessionId, view) =>
        set((s) => {
          const current = s.dock[sessionId] ?? DEFAULT_DOCK;
          if (current.view === view && current.open) return s;
          return { dock: pruneDock({ ...s.dock, [sessionId]: { open: true, view } }) };
        }),

      setDockWidth: (width) => set({ dockWidth: Math.max(300, Math.min(900, width)) }),
      setDockMaximized: (maximized) => set({ dockMaximized: maximized }),

      setSyncDockExpanded: (expanded) => set({ syncDockExpanded: expanded }),

      setTheme: (theme) => set({ theme }),
    }),
    {
      name: "agentique:ui",
      version: 6,
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
        if (version < 6) {
          // The two global toggles (browser / workflow) and the todo sidebar
          // became one per-session dock. There is no session id to attach the
          // old global choice to, so only the width — a viewport preference —
          // survives; every session starts with a closed dock.
          state.dockWidth = (state.browserPanelWidth as number | undefined) ?? 500;
          state.dock = {};
          state.dockMaximized = false;
          delete state.browserPanelWidth;
          delete state.rightPanelCollapsed;
          delete state.rightPanelView;
          delete state.todoSidebarCollapsed;
        }
        return state;
      },
      partialize: (state) => ({
        drafts: state.drafts,
        stashes: state.stashes,
        legacyPinnedProjectIds: state.legacyPinnedProjectIds,
        dock: state.dock,
        dockWidth: state.dockWidth,
        dockMaximized: state.dockMaximized,
        syncDockExpanded: state.syncDockExpanded,
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
