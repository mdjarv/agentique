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
  effort: "xhigh",
};

interface UIState {
  drafts: Record<string, string>;
  stashes: Record<string, string[]>;
  expandedProjects: Record<string, boolean>;
  expandedFolders: Record<string, boolean>;
  pinnedProjectIds: string[];
  sidebarFocusMode: boolean;
  rightPanelCollapsed: boolean;
  browserPanelWidth: number;
  theme: Theme;

  setDraft: (sessionId: string, text: string) => void;
  clearDraft: (sessionId: string) => void;
  pushStash: (sessionId: string, text: string) => void;
  popStash: (sessionId: string) => string | undefined;
  clearStash: (sessionId: string) => void;
  setProjectExpanded: (projectId: string, expanded: boolean) => void;
  setManyProjectsExpanded: (projectIds: string[], expanded: boolean) => void;
  setFolderExpanded: (folderName: string, expanded: boolean) => void;
  setManyFoldersExpanded: (folderNames: string[], expanded: boolean) => void;
  renameFolderExpanded: (oldName: string, newName: string) => void;
  toggleProjectPinned: (projectId: string) => void;
  setSidebarFocusMode: (enabled: boolean) => void;
  setRightPanelCollapsed: (collapsed: boolean) => void;
  setBrowserPanelWidth: (width: number) => void;
  setTheme: (theme: Theme) => void;
}

export const useUIStore = create<UIState>()(
  persist(
    (set, get) => ({
      drafts: {},
      stashes: {},
      expandedProjects: Object.fromEntries(readLegacyCollapsedProjects().map((id) => [id, false])),
      expandedFolders: {},
      pinnedProjectIds: [],
      sidebarFocusMode: false,
      rightPanelCollapsed: true,
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

      setProjectExpanded: (projectId, expanded) =>
        set((s) => ({ expandedProjects: { ...s.expandedProjects, [projectId]: expanded } })),

      setManyProjectsExpanded: (projectIds, expanded) =>
        set((s) => {
          if (projectIds.length === 0) return s;
          const next = { ...s.expandedProjects };
          let changed = false;
          for (const id of projectIds) {
            if (next[id] !== expanded) {
              next[id] = expanded;
              changed = true;
            }
          }
          return changed ? { expandedProjects: next } : s;
        }),

      setFolderExpanded: (folderName, expanded) =>
        set((s) => ({ expandedFolders: { ...s.expandedFolders, [folderName]: expanded } })),

      setManyFoldersExpanded: (folderNames, expanded) =>
        set((s) => {
          if (folderNames.length === 0) return s;
          const next = { ...s.expandedFolders };
          let changed = false;
          for (const name of folderNames) {
            if (next[name] !== expanded) {
              next[name] = expanded;
              changed = true;
            }
          }
          return changed ? { expandedFolders: next } : s;
        }),

      renameFolderExpanded: (oldName, newName) =>
        set((s) => {
          if (!(oldName in s.expandedFolders)) return s;
          const { [oldName]: value, ...rest } = s.expandedFolders;
          return { expandedFolders: { ...rest, [newName]: value ?? true } };
        }),

      toggleProjectPinned: (projectId) =>
        set((s) => {
          const has = s.pinnedProjectIds.includes(projectId);
          return {
            pinnedProjectIds: has
              ? s.pinnedProjectIds.filter((id) => id !== projectId)
              : [...s.pinnedProjectIds, projectId],
          };
        }),

      setSidebarFocusMode: (enabled) => set({ sidebarFocusMode: enabled }),

      setRightPanelCollapsed: (collapsed) => set({ rightPanelCollapsed: collapsed }),

      setBrowserPanelWidth: (width) =>
        set({ browserPanelWidth: Math.max(300, Math.min(900, width)) }),

      setTheme: (theme) => set({ theme }),
    }),
    {
      name: "agentique:ui",
      version: 3,
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
        return state;
      },
      partialize: (state) => ({
        drafts: state.drafts,
        stashes: state.stashes,
        expandedProjects: state.expandedProjects,
        expandedFolders: state.expandedFolders,
        pinnedProjectIds: state.pinnedProjectIds,
        sidebarFocusMode: state.sidebarFocusMode,
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
