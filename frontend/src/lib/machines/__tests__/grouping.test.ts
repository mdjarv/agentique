import { describe, expect, it } from "vitest";
import { groupProjects } from "~/lib/machines/grouping";
import type { Project } from "~/lib/types";

function project(p: Partial<Project> & { id: string }): Project {
  return {
    name: p.id,
    path: `/x/${p.id}`,
    default_model: "",
    default_permission_mode: "",
    default_system_prompt: "",
    created_at: "",
    updated_at: "",
    slug: p.id,
    sort_order: 0,
    default_behavior_presets: "",
    favorite: 0,
    color: "",
    icon: "",
    folder: "",
    max_sessions: 0,
    pinned: 0,
    remote_url: "",
    ...p,
  } as Project;
}

describe("groupProjects", () => {
  it("merges same-remote projects across machines with the primary as representative", () => {
    const local = project({ id: "l", remote_url: "github.com/org/repo" });
    const remote = project({ id: "r", remote_url: "github.com/org/repo", machineId: "m1" });
    // Remote listed first — representative choice must not depend on order.
    const groups = groupProjects([remote, local]);
    expect(groups).toHaveLength(1);
    expect(groups[0]?.project.id).toBe("l");
    expect(groups[0]?.members.map((m) => m.id)).toEqual(["l", "r"]);
  });

  it("keeps remote-only groups with the remote as representative", () => {
    const remote = project({ id: "r", remote_url: "github.com/org/solo", machineId: "m1" });
    const groups = groupProjects([remote]);
    expect(groups[0]?.project.id).toBe("r");
  });

  it("never groups projects without a remote", () => {
    const a = project({ id: "a" });
    const b = project({ id: "b" });
    expect(groupProjects([a, b])).toHaveLength(2);
  });

  it("keeps distinct remotes (and monorepo-subdir keys) apart", () => {
    const root = project({ id: "root", remote_url: "github.com/org/mono" });
    const ui = project({ id: "ui", remote_url: "github.com/org/mono::packages/ui" });
    const core = project({ id: "core", remote_url: "github.com/org/mono::packages/core" });
    expect(groupProjects([root, ui, core])).toHaveLength(3);
  });

  it("preserves first-seen order of groups", () => {
    const a = project({ id: "a", remote_url: "github.com/org/a" });
    const b = project({ id: "b", remote_url: "github.com/org/b" });
    const a2 = project({ id: "a2", remote_url: "github.com/org/a", machineId: "m1" });
    expect(groupProjects([a, b, a2]).map((g) => g.project.id)).toEqual(["a", "b"]);
  });
});
