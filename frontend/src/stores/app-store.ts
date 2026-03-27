import { create } from "zustand";
import type { Project, ProjectGitStatus } from "~/lib/generated-types";

export type { ProjectGitStatus } from "~/lib/generated-types";

interface AppState {
  projects: Project[];
  projectsLoaded: boolean;
  sidebarOpen: boolean;
  projectGitStatus: Record<string, ProjectGitStatus>;
  setProjects: (projects: Project[]) => void;
  addProject: (project: Project) => void;
  updateProject: (project: Project) => void;
  removeProject: (id: string) => void;
  reorderProjects: (orderedIds: string[]) => void;
  setSidebarOpen: (open: boolean) => void;
  setProjectGitStatus: (status: ProjectGitStatus) => void;
}

export const useAppStore = create<AppState>((set) => ({
  projects: [],
  projectsLoaded: false,
  projectGitStatus: {},
  sidebarOpen: false,
  setProjects: (projects) => set({ projects, projectsLoaded: true }),
  addProject: (project) => set((state) => ({ projects: [...state.projects, project] })),
  updateProject: (project) =>
    set((state) => ({ projects: state.projects.map((p) => (p.id === project.id ? project : p)) })),
  removeProject: (id) => set((state) => ({ projects: state.projects.filter((p) => p.id !== id) })),
  reorderProjects: (orderedIds) =>
    set((state) => {
      const byId = new Map(state.projects.map((p) => [p.id, p]));
      const reordered: Project[] = [];
      for (const id of orderedIds) {
        const p = byId.get(id);
        if (p) reordered.push(p);
      }
      for (const p of state.projects) {
        if (!byId.has(p.id) || reordered.includes(p)) continue;
        reordered.push(p);
      }
      return { projects: reordered };
    }),
  setSidebarOpen: (open) => set({ sidebarOpen: open }),
  setProjectGitStatus: (status) =>
    set((state) => ({
      projectGitStatus: { ...state.projectGitStatus, [status.projectId]: status },
    })),
}));
