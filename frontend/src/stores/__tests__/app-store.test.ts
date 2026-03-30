import { beforeEach, describe, expect, it } from "vitest";
import type { Project, ProjectGitStatus, ProjectTag, Tag } from "~/lib/generated-types";
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

function makeTag(overrides: Partial<Tag> = {}): Tag {
  return {
    id: "tag-1",
    name: "Frontend",
    color: "#ff0000",
    sort_order: 0,
    created_at: "2024-01-01T00:00:00Z",
    updated_at: "2024-01-01T00:00:00Z",
    ...overrides,
  };
}

describe("app-store", () => {
  beforeEach(() => {
    useAppStore.setState({
      projects: [],
      projectsLoaded: false,
      projectGitStatus: {},
      sidebarOpen: false,
      tags: [],
      projectTags: [],
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

  describe("tags", () => {
    it("setTags replaces all tags", () => {
      useAppStore.getState().setTags([makeTag({ id: "t1" }), makeTag({ id: "t2" })]);
      expect(useAppStore.getState().tags).toHaveLength(2);
    });

    it("addTag appends and deduplicates", () => {
      useAppStore.getState().setTags([makeTag({ id: "t1" })]);
      useAppStore.getState().addTag(makeTag({ id: "t2" }));
      expect(useAppStore.getState().tags).toHaveLength(2);

      useAppStore.getState().addTag(makeTag({ id: "t1" }));
      expect(useAppStore.getState().tags).toHaveLength(2);
    });

    it("updateTag replaces by id", () => {
      useAppStore.getState().setTags([makeTag({ id: "t1", name: "Old" })]);
      useAppStore.getState().updateTag(makeTag({ id: "t1", name: "New" }));
      expect(useAppStore.getState().tags[0]?.name).toBe("New");
    });

    it("removeTag removes tag and cascades to projectTags", () => {
      useAppStore.getState().setTags([makeTag({ id: "t1" }), makeTag({ id: "t2" })]);
      useAppStore.getState().setProjectTags([
        { project_id: "p1", tag_id: "t1" },
        { project_id: "p1", tag_id: "t2" },
      ]);
      useAppStore.getState().removeTag("t1");
      expect(useAppStore.getState().tags).toHaveLength(1);
      expect(useAppStore.getState().projectTags).toHaveLength(1);
      expect(useAppStore.getState().projectTags[0]?.tag_id).toBe("t2");
    });
  });

  describe("projectTags", () => {
    it("setProjectTags replaces all", () => {
      const pts: ProjectTag[] = [{ project_id: "p1", tag_id: "t1" }];
      useAppStore.getState().setProjectTags(pts);
      expect(useAppStore.getState().projectTags).toEqual(pts);
    });

    it("setTagsForProject replaces tags for a specific project", () => {
      useAppStore.getState().setProjectTags([
        { project_id: "p1", tag_id: "t1" },
        { project_id: "p2", tag_id: "t2" },
      ]);
      useAppStore.getState().setTagsForProject("p1", ["t3", "t4"]);
      const pts = useAppStore.getState().projectTags;
      expect(pts).toHaveLength(3);
      expect(pts.filter((pt) => pt.project_id === "p1")).toHaveLength(2);
      expect(pts.find((pt) => pt.project_id === "p2")?.tag_id).toBe("t2");
    });
  });
});
