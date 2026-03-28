import { create } from "zustand";
import type { Project, ProjectGitStatus, ProjectTag, Tag } from "~/lib/generated-types";

export type { ProjectGitStatus } from "~/lib/generated-types";

interface AppState {
  projects: Project[];
  projectsLoaded: boolean;
  sidebarOpen: boolean;
  projectGitStatus: Record<string, ProjectGitStatus>;
  tags: Tag[];
  projectTags: ProjectTag[];

  setProjects: (projects: Project[]) => void;
  addProject: (project: Project) => void;
  updateProject: (project: Project) => void;
  removeProject: (id: string) => void;
  setSidebarOpen: (open: boolean) => void;
  setProjectGitStatus: (status: ProjectGitStatus) => void;

  setTags: (tags: Tag[]) => void;
  addTag: (tag: Tag) => void;
  updateTag: (tag: Tag) => void;
  removeTag: (id: string) => void;
  setProjectTags: (projectTags: ProjectTag[]) => void;
  setTagsForProject: (projectId: string, tagIds: string[]) => void;
}

export const useAppStore = create<AppState>((set) => ({
  projects: [],
  projectsLoaded: false,
  projectGitStatus: {},
  sidebarOpen: false,
  tags: [],
  projectTags: [],

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

  setTags: (tags) => set({ tags }),
  addTag: (tag) => set((state) => ({ tags: [...state.tags, tag] })),
  updateTag: (tag) =>
    set((state) => ({ tags: state.tags.map((t) => (t.id === tag.id ? tag : t)) })),
  removeTag: (id) =>
    set((state) => ({
      tags: state.tags.filter((t) => t.id !== id),
      projectTags: state.projectTags.filter((pt) => pt.tag_id !== id),
    })),
  setProjectTags: (projectTags) => set({ projectTags }),
  setTagsForProject: (projectId, tagIds) =>
    set((state) => ({
      projectTags: [
        ...state.projectTags.filter((pt) => pt.project_id !== projectId),
        ...tagIds.map((tagId) => ({ project_id: projectId, tag_id: tagId })),
      ],
    })),
}));
