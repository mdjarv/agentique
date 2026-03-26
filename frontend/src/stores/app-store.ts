import { create } from "zustand";
import type { Project } from "~/lib/types";

export interface ProjectGitStatus {
  projectId: string;
  branch: string;
  hasRemote: boolean;
  aheadRemote: number;
  behindRemote: number;
  uncommittedCount: number;
}

interface AppState {
  projects: Project[];
  projectsLoaded: boolean;
  sidebarOpen: boolean;
  projectGitStatus: Record<string, ProjectGitStatus>;
  setProjects: (projects: Project[]) => void;
  addProject: (project: Project) => void;
  updateProject: (project: Project) => void;
  removeProject: (id: string) => void;
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
  setSidebarOpen: (open) => set({ sidebarOpen: open }),
  setProjectGitStatus: (status) =>
    set((state) => ({
      projectGitStatus: { ...state.projectGitStatus, [status.projectId]: status },
    })),
}));
