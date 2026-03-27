import { create } from "zustand";
import { createJSONStorage, persist } from "zustand/middleware";

const LEGACY_COLLAPSED_KEY = "agentique:collapsed-projects";

function readLegacyCollapsedProjects(): string[] {
  try {
    const raw = localStorage.getItem(LEGACY_COLLAPSED_KEY);
    if (raw) return JSON.parse(raw) as string[];
  } catch {}
  return [];
}

interface UIState {
  drafts: Record<string, string>;
  collapsedProjectIds: string[];

  setDraft: (sessionId: string, text: string) => void;
  clearDraft: (sessionId: string) => void;
  setProjectCollapsed: (projectId: string, collapsed: boolean) => void;
}

export const useUIStore = create<UIState>()(
  persist(
    (set) => ({
      drafts: {},
      collapsedProjectIds: readLegacyCollapsedProjects(),

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
    }),
    {
      name: "agentique:ui",
      storage: createJSONStorage(() => localStorage),
      partialize: (state) => ({
        drafts: state.drafts,
        collapsedProjectIds: state.collapsedProjectIds,
      }),
      onRehydrateStorage: () => () => {
        try {
          localStorage.removeItem(LEGACY_COLLAPSED_KEY);
        } catch {}
      },
    },
  ),
);
