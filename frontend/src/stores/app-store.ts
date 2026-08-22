import { create } from "zustand";
import type { ProjectGitStatus } from "~/lib/generated-types";
import { remoteSlug } from "~/lib/machines/slug";
import type { Project } from "~/lib/types";

export type { ProjectGitStatus } from "~/lib/generated-types";

interface AppState {
  projects: Project[];
  projectsLoaded: boolean;
  sidebarOpen: boolean;
  projectGitStatus: Record<string, ProjectGitStatus>;
  /**
   * projectId → epoch ms of the last real `git fetch` for that project.
   * Ahead/behind is only as true as its last fetch, so the sync dock reports
   * this age rather than presenting a stale count as fact.
   */
  projectGitFetchedAt: Record<string, number>;

  setProjects: (projects: Project[]) => void;
  /** Replace one remote machine's projects, leaving all other machines' (and
   *  the primary's) untouched. */
  setMachineProjects: (machineId: string, projects: Project[]) => void;
  removeMachineProjects: (machineId: string) => void;
  addProject: (project: Project) => void;
  updateProject: (project: Project) => void;
  removeProject: (id: string) => void;
  setSidebarOpen: (open: boolean) => void;
  setProjectGitStatus: (status: ProjectGitStatus) => void;
  markProjectFetched: (projectId: string, at: number) => void;
}

export const useAppStore = create<AppState>((set) => ({
  projects: [],
  projectsLoaded: false,
  projectGitStatus: {},
  projectGitFetchedAt: {},
  sidebarOpen: false,

  // Primary-machine load: replaces only untagged projects so a primary
  // refetch can't wipe already-loaded remote machines' entries.
  setProjects: (projects) =>
    set((state) => ({
      projects: [...projects, ...state.projects.filter((p) => p.machineId)],
      projectsLoaded: true,
    })),
  setMachineProjects: (machineId, projects) =>
    set((state) => ({
      projects: [...state.projects.filter((p) => p.machineId !== machineId), ...projects],
    })),
  removeMachineProjects: (machineId) =>
    set((state) => ({ projects: state.projects.filter((p) => p.machineId !== machineId) })),
  addProject: (project) => set((state) => ({ projects: [...state.projects, project] })),
  updateProject: (project) =>
    set((state) => ({
      projects: state.projects.map((p) => {
        if (p.id !== project.id) return p;
        // project.updated pushes arrive untagged from whichever socket emitted
        // them — reapply the client-side machine tag and slug qualifier.
        return p.machineId
          ? { ...project, machineId: p.machineId, slug: remoteSlug(project.slug, p.machineId) }
          : project;
      }),
    })),
  removeProject: (id) => set((state) => ({ projects: state.projects.filter((p) => p.id !== id) })),
  setSidebarOpen: (open) => set({ sidebarOpen: open }),
  setProjectGitStatus: (status) =>
    set((state) => ({
      projectGitStatus: { ...state.projectGitStatus, [status.projectId]: status },
    })),
  markProjectFetched: (projectId, at) =>
    set((state) => ({
      projectGitFetchedAt: { ...state.projectGitFetchedAt, [projectId]: at },
    })),
}));
