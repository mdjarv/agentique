import { create } from "zustand";
import { createJSONStorage, persist } from "zustand/middleware";
import type { EffortLevel } from "~/components/chat/MessageComposer";
import type { ModelId } from "~/lib/session-actions";

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
  autoApprove: boolean;
  model: ModelId;
  effort: EffortLevel;
}

const DEFAULT_SESSION_DEFAULTS: SessionDefaults = {
  worktree: true,
  planMode: false,
  autoApprove: true,
  model: "opus",
  effort: "",
};

interface UIState {
  drafts: Record<string, string>;
  collapsedProjectIds: string[];
  sessionDefaults: SessionDefaults;

  setDraft: (sessionId: string, text: string) => void;
  clearDraft: (sessionId: string) => void;
  setProjectCollapsed: (projectId: string, collapsed: boolean) => void;
  setSessionDefaults: (partial: Partial<SessionDefaults>) => void;
}

export const useUIStore = create<UIState>()(
  persist(
    (set) => ({
      drafts: {},
      collapsedProjectIds: readLegacyCollapsedProjects(),
      sessionDefaults: { ...DEFAULT_SESSION_DEFAULTS },

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

      setSessionDefaults: (partial) =>
        set((s) => ({ sessionDefaults: { ...s.sessionDefaults, ...partial } })),
    }),
    {
      name: "agentique:ui",
      storage: createJSONStorage(() => localStorage),
      partialize: (state) => ({
        drafts: state.drafts,
        collapsedProjectIds: state.collapsedProjectIds,
        sessionDefaults: state.sessionDefaults,
      }),
      onRehydrateStorage: () => () => {
        try {
          localStorage.removeItem(LEGACY_COLLAPSED_KEY);
        } catch {}
      },
    },
  ),
);
