import { describe, expect, it } from "vitest";
import type { ProjectGitStatus } from "~/lib/generated-types";
import type { Project } from "~/lib/types";
import {
  deriveAction,
  deriveSyncRows,
  mechanicalRows,
  type SyncRowInput,
  summarize,
} from "../sync-derive";

function project(overrides: Partial<Project> = {}): Project {
  return {
    id: "p-1",
    name: "Agentique",
    path: "/repo",
    default_model: "",
    default_permission_mode: "",
    default_system_prompt: "",
    created_at: "",
    updated_at: "",
    slug: "agentique",
    sort_order: 0,
    default_behavior_presets: "",
    favorite: 0,
    color: "",
    icon: "",
    folder: "",
    max_sessions: 0,
    pinned: 0,
    remote_url: "github.com/org/agentique",
    ...overrides,
  };
}

function status(overrides: Partial<ProjectGitStatus> = {}): ProjectGitStatus {
  return {
    projectId: "p-1",
    branch: "master",
    hasRemote: true,
    aheadRemote: 0,
    behindRemote: 0,
    uncommittedCount: 0,
    ...overrides,
  };
}

function input(overrides: Partial<SyncRowInput> = {}): SyncRowInput {
  return {
    project: project(),
    status: status({ aheadRemote: 3 }),
    colorBg: "#9ece6a",
    colorFg: "#9ece6a",
    ...overrides,
  };
}

describe("deriveAction", () => {
  it("calls a clean behind-only checkout a fast-forward pull", () => {
    expect(deriveAction(status({ behindRemote: 4 }))).toBe("pull");
  });

  it("calls ahead-only a push", () => {
    expect(deriveAction(status({ aheadRemote: 2 }))).toBe("push");
  });

  // Both messy cases must stay off the one-click path: a pull that has to
  // replay local commits, and one that would trample uncommitted work.
  it("calls diverged and dirty-while-behind a rebase", () => {
    expect(deriveAction(status({ aheadRemote: 1, behindRemote: 2 }))).toBe("rebase");
    expect(deriveAction(status({ behindRemote: 2, uncommittedCount: 4 }))).toBe("rebase");
  });

  it("still calls ahead-with-dirt a push — the commits are already made", () => {
    expect(deriveAction(status({ aheadRemote: 2, uncommittedCount: 5 }))).toBe("push");
  });
});

describe("deriveSyncRows", () => {
  it("keeps only checkouts that have drifted from a remote", () => {
    const rows = deriveSyncRows([
      input({ status: status({ aheadRemote: 3 }) }),
      input({ project: project({ id: "p-2", slug: "clean" }), status: status() }),
      input({
        project: project({ id: "p-3", slug: "no-remote" }),
        status: status({ hasRemote: false, aheadRemote: 9 }),
      }),
      input({ project: project({ id: "p-4", slug: "unknown" }), status: undefined }),
    ]);
    expect(rows.map((r) => r.projectId)).toEqual(["p-1"]);
  });

  // Uncommitted work is not a sync problem — docking it would keep half the
  // repos permanently listed.
  it("ignores a checkout that is only dirty", () => {
    const rows = deriveSyncRows([input({ status: status({ uncommittedCount: 7 }) })]);
    expect(rows).toEqual([]);
  });

  it("orders mechanical work first, messy last, slug-stable within a rank", () => {
    const rows = deriveSyncRows([
      input({
        project: project({ id: "p-r", slug: "zeta" }),
        status: status({ aheadRemote: 1, behindRemote: 1 }),
      }),
      input({ project: project({ id: "p-b", slug: "beta" }), status: status({ behindRemote: 5 }) }),
      input({ project: project({ id: "p-a", slug: "alpha" }), status: status({ aheadRemote: 2 }) }),
      input({ project: project({ id: "p-c", slug: "gamma" }), status: status({ aheadRemote: 1 }) }),
    ]);
    expect(rows.map((r) => r.slug)).toEqual(["alpha", "gamma", "beta", "zeta"]);
  });
});

describe("summarize", () => {
  it("counts actions but de-duplicates repos into chips", () => {
    // The same repo, drifted on two machines: two rows, one face.
    const rows = deriveSyncRows([
      input({ status: status({ aheadRemote: 3 }) }),
      input({
        project: project({ id: "p-1-zbook", slug: "agentique~ad3e932", machineId: "m-z" }),
        status: status({ projectId: "p-1-zbook", aheadRemote: 1, behindRemote: 2 }),
        machineLabel: "zbook",
      }),
      input({
        project: project({ id: "p-2", slug: "agentkit", remote_url: "github.com/org/agentkit" }),
        status: status({ aheadRemote: 5 }),
      }),
    ]);
    const summary = summarize(rows);
    expect(summary.total).toBe(3);
    expect(summary.chips.map((c) => c.slug)).toEqual(["agentique", "agentkit"]);
  });

  it("counts only one-click work as mechanical", () => {
    const rows = deriveSyncRows([
      input({ status: status({ aheadRemote: 3 }) }),
      input({
        project: project({ id: "p-2", slug: "beta", remote_url: "github.com/org/beta" }),
        status: status({ aheadRemote: 1, behindRemote: 1 }),
      }),
    ]);
    expect(summarize(rows).mechanical).toBe(1);
    expect(mechanicalRows(rows).map((r) => r.slug)).toEqual(["agentique"]);
  });

  it("groups by project id when a repo has no canonical remote", () => {
    const rows = deriveSyncRows([
      input({ project: project({ remote_url: "" }), status: status({ aheadRemote: 1 }) }),
      input({
        project: project({ id: "p-2", slug: "other", remote_url: "" }),
        status: status({ aheadRemote: 1 }),
      }),
    ]);
    expect(summarize(rows).chips).toHaveLength(2);
  });
});
