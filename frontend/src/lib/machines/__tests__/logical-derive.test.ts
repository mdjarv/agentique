import { describe, expect, it } from "vitest";
import type { MachineFacts } from "~/lib/machines/logical-derive";
import {
  compareLogicalProjects,
  deriveLogicalProjects,
  matchesLogicalProject,
} from "~/lib/machines/logical-derive";
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

const REPO = "github.com/org/repo";
const online: Record<string, MachineFacts> = {
  m1: { label: "zbook", icon: "laptop", status: "connected" },
};
const away: Record<string, MachineFacts> = {
  m1: { label: "zbook", icon: "laptop", status: "disconnected" },
};

describe("deriveLogicalProjects", () => {
  it("renders one row per repo, presented by the primary's copy", () => {
    const local = project({
      id: "l",
      name: "agentique",
      slug: "agentique",
      color: "indigo",
      icon: "bot",
      remote_url: REPO,
    });
    const remote = project({
      id: "r",
      name: "Agentique",
      slug: "agentique~m1",
      color: "rose",
      icon: "cpu",
      favorite: 1,
      remote_url: REPO,
      machineId: "m1",
    });

    const rows = deriveLogicalProjects([remote, local], online);
    expect(rows).toHaveLength(1);
    const row = rows[0];
    expect(row?.id).toBe("l");
    expect(row?.name).toBe("agentique");
    expect(row?.slug).toBe("agentique");
    expect(row?.color).toBe("indigo");
    expect(row?.icon).toBe("bot");
    expect(row?.spansMachines).toBe(true);
    expect(row?.remoteMembers.map((m) => m.machineLabel)).toEqual(["zbook"]);
  });

  it("ignores a remote copy's favorite — the host serving the UI owns the star", () => {
    const local = project({ id: "l", remote_url: REPO });
    const remote = project({ id: "r", favorite: 1, remote_url: REPO, machineId: "m1" });
    expect(deriveLogicalProjects([local, remote], online)[0]?.favorite).toBe(false);

    const starred = project({ id: "l", favorite: 1, remote_url: REPO });
    expect(deriveLogicalProjects([starred, remote], online)[0]?.favorite).toBe(true);
  });

  it("is away only when EVERY member's machine is away", () => {
    const local = project({ id: "l", remote_url: REPO });
    const remote = project({ id: "r", remote_url: REPO, machineId: "m1" });

    expect(deriveLogicalProjects([local, remote], away)[0]?.away).toBe(false);
    expect(deriveLogicalProjects([remote], away)[0]?.away).toBe(true);
    expect(deriveLogicalProjects([remote], online)[0]?.away).toBe(false);
  });

  it("treats an unknown machine as away rather than assuming it is reachable", () => {
    const remote = project({ id: "r", remote_url: REPO, machineId: "ghost" });
    const row = deriveLogicalProjects([remote], online)[0];
    expect(row?.away).toBe(true);
    expect(row?.members[0]?.machineLabel).toBe("Unknown machine");
  });

  it("keeps a single-machine repo free of multi-machine chrome", () => {
    const solo = project({ id: "l", remote_url: REPO });
    const row = deriveLogicalProjects([solo], online)[0];
    expect(row?.spansMachines).toBe(false);
    expect(row?.remoteMembers).toEqual([]);
  });

  it("keeps every member addressable — commands target a physical checkout", () => {
    const local = project({ id: "l", remote_url: REPO, path: "/home/me/agentique" });
    const remote = project({
      id: "r",
      slug: "agentique~m1",
      remote_url: REPO,
      machineId: "m1",
      path: "/Users/me/src/agentique",
    });
    const row = deriveLogicalProjects([local, remote], online)[0];
    expect(row?.members.map((m) => [m.projectId, m.slug, m.path])).toEqual([
      ["l", "l", "/home/me/agentique"],
      ["r", "agentique~m1", "/Users/me/src/agentique"],
    ]);
  });
});

describe("matchesLogicalProject", () => {
  const local = project({ id: "l", name: "agentique", slug: "agentique", remote_url: REPO });
  const remote = project({
    id: "r",
    name: "Agentique",
    slug: "agentique~m1",
    path: "/srv/code/agentique",
    remote_url: REPO,
    machineId: "m1",
  });
  const byId = new Map([local, remote].map((p) => [p.id, p]));
  const row = deriveLogicalProjects([local, remote], online)[0];

  it("matches on any member's name or slug", () => {
    if (!row) throw new Error("no row");
    expect(matchesLogicalProject(row, byId, "agent")).toBe(true);
    expect(matchesLogicalProject(row, byId, "m1")).toBe(true);
    expect(matchesLogicalProject(row, byId, "nope")).toBe(false);
  });

  it("matches paths only when asked (the inventory, not the palette)", () => {
    if (!row) throw new Error("no row");
    expect(matchesLogicalProject(row, byId, "/srv/code")).toBe(false);
    expect(matchesLogicalProject(row, byId, "/srv/code", true)).toBe(true);
  });

  it("keeps an empty query inclusive", () => {
    if (!row) throw new Error("no row");
    expect(matchesLogicalProject(row, byId, "   ")).toBe(true);
  });
});

describe("compareLogicalProjects", () => {
  it("puts favorites first, then sorts by name", () => {
    const rows = deriveLogicalProjects(
      [
        project({ id: "b", name: "beta", remote_url: "github.com/org/b" }),
        project({ id: "a", name: "alpha", remote_url: "github.com/org/a" }),
        project({ id: "z", name: "zeta", favorite: 1, remote_url: "github.com/org/z" }),
      ],
      online,
    ).sort(compareLogicalProjects);
    expect(rows.map((r) => r.name)).toEqual(["zeta", "alpha", "beta"]);
  });
});
