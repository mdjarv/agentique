import { beforeEach, describe, expect, it } from "vitest";
import type { Project, ProjectGitStatus } from "~/lib/generated-types";
import { useAppStore } from "~/stores/app-store";

function makeProject(overrides: Partial<Project> = {}): Project {
  return {
    id: "p1",
    name: "Test",
    path: "/tmp/test",
    default_model: "sonnet",
    default_permission_mode: "default",
    default_system_prompt: "",
    created_at: "2024-01-01T00:00:00Z",
    updated_at: "2024-01-01T00:00:00Z",
    slug: "test",
    sort_order: 0,
    is_favorite: false,
    behavior_presets: "",
    ...overrides,
  } as Project;
}

describe("app-store", () => {
  beforeEach(() => {
    useAppStore.setState({
      projects: [],
      projectsLoaded: false,
      projectGitStatus: {},
      sidebarOpen: false,
    });
  });

  describe("projects", () => {
    it("setProjects sets projects and marks loaded", () => {
      useAppStore.getState().setProjects([makeProject()]);
      expect(useAppStore.getState().projects).toHaveLength(1);
      expect(useAppStore.getState().projectsLoaded).toBe(true);
    });

    it("addProject appends", () => {
      useAppStore.getState().setProjects([makeProject({ id: "p1" })]);
      useAppStore.getState().addProject(makeProject({ id: "p2" }));
      expect(useAppStore.getState().projects).toHaveLength(2);
    });

    it("updateProject replaces by id", () => {
      useAppStore.getState().setProjects([makeProject({ id: "p1", name: "Old" })]);
      useAppStore.getState().updateProject(makeProject({ id: "p1", name: "New" }));
      expect(useAppStore.getState().projects[0]?.name).toBe("New");
    });

    it("removeProject filters by id", () => {
      useAppStore.getState().setProjects([makeProject({ id: "p1" }), makeProject({ id: "p2" })]);
      useAppStore.getState().removeProject("p1");
      expect(useAppStore.getState().projects).toHaveLength(1);
      expect(useAppStore.getState().projects[0]?.id).toBe("p2");
    });
  });

  describe("projectGitStatus", () => {
    it("stores by projectId", () => {
      const status: ProjectGitStatus = {
        projectId: "p1",
        branch: "main",
        hasRemote: true,
        aheadRemote: 1,
        behindRemote: 0,
        uncommittedCount: 3,
      };
      useAppStore.getState().setProjectGitStatus(status);
      expect(useAppStore.getState().projectGitStatus.p1).toEqual(status);
    });
  });
});
