import { create } from "zustand";
import type { Project } from "~/lib/types";

interface AppState {
  projects: Project[];
  setProjects: (projects: Project[]) => void;
  addProject: (project: Project) => void;
  removeProject: (id: string) => void;
  hideStoppedSessions: boolean;
  toggleHideStoppedSessions: () => void;
}

export const useAppStore = create<AppState>((set) => ({
  projects: [],
  setProjects: (projects) => set({ projects }),
  addProject: (project) => set((state) => ({ projects: [...state.projects, project] })),
  removeProject: (id) => set((state) => ({ projects: state.projects.filter((p) => p.id !== id) })),
  hideStoppedSessions: false,
  toggleHideStoppedSessions: () =>
    set((state) => ({ hideStoppedSessions: !state.hideStoppedSessions })),
}));
