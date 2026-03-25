import { create } from "zustand";
import type { Project } from "~/lib/types";

interface AppState {
  projects: Project[];
  projectsLoaded: boolean;
  setProjects: (projects: Project[]) => void;
  addProject: (project: Project) => void;
  updateProject: (project: Project) => void;
  removeProject: (id: string) => void;
}

export const useAppStore = create<AppState>((set) => ({
  projects: [],
  projectsLoaded: false,
  setProjects: (projects) => set({ projects, projectsLoaded: true }),
  addProject: (project) => set((state) => ({ projects: [...state.projects, project] })),
  updateProject: (project) =>
    set((state) => ({ projects: state.projects.map((p) => (p.id === project.id ? project : p)) })),
  removeProject: (id) => set((state) => ({ projects: state.projects.filter((p) => p.id !== id) })),
}));
