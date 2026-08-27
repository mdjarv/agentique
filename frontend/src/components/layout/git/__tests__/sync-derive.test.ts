import { describe, expect, it } from "vitest";
import type { ProjectGitStatus } from "~/lib/generated-types";
import type { Project } from "~/lib/types";
import {
  bulkLabel,
  bulkPlan,
  bulkTargets,
  deriveAction,
  deriveSyncRows,
  exceptionRows,
  mechanicalRows,
  type SyncRowInput,
  summarize,
  syncSegments,
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

  // The label is the project's name, which carries no machine suffix to drop;
  // routing keeps the qualified slug, which is the one that is actually unique.
  it("labels a remote checkout by name while routing by its qualified slug", () => {
    const [row] = deriveSyncRows([
      input({
        project: project({
          id: "p-z",
          name: "alltix-ui",
          slug: "alltix-ui~ad3e932",
          machineId: "m-z",
        }),
        status: status({ behindRemote: 31 }),
        machineLabel: "zbook",
      }),
    ]);
    expect(row?.slug).toBe("alltix-ui~ad3e932");
    expect(row?.label).toBe("alltix-ui");
    expect(row?.initials).toBe("AU");
  });

  // The whole point of the change: a renamed project reads by its name, not by
  // the ASCII slug derived from whatever it was called when it was created.
  it("labels by the name, not the slug it was registered under", () => {
    const [row] = deriveSyncRows([
      input({ project: project({ id: "p-t", name: "Träffbild", slug: "traffbild" }) }),
    ]);
    expect(row?.slug).toBe("traffbild");
    expect(row?.label).toBe("Träffbild");
    expect(row?.initials).toBe("TR");
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
        project: project({
          id: "p-2",
          name: "agentkit",
          slug: "agentkit",
          remote_url: "github.com/org/agentkit",
        }),
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

describe("syncSegments", () => {
  it("colours a diverged checkout's commits amber, whichever way they point", () => {
    const rows = deriveSyncRows([
      input({ status: status({ aheadRemote: 12 }) }),
      input({
        project: project({
          id: "p-2",
          name: "webticket-ui",
          slug: "webticket-ui",
          remote_url: "github.com/org/wt",
        }),
        status: status({ projectId: "p-2", behindRemote: 3 }),
      }),
      input({
        project: project({
          id: "p-3",
          name: "alltix-api",
          slug: "alltix-api",
          remote_url: "github.com/org/ax",
        }),
        status: status({ projectId: "p-3", aheadRemote: 2, behindRemote: 3 }),
      }),
    ]);
    expect(syncSegments(rows)).toEqual({ ahead: 12, behind: 3, diverged: 5, total: 20 });
  });

  it("is all zeroes with nothing docked", () => {
    expect(syncSegments([])).toEqual({ ahead: 0, behind: 0, diverged: 0, total: 0 });
  });
});

describe("bulkPlan / bulkLabel", () => {
  const pushes = () =>
    deriveSyncRows([
      input({ status: status({ aheadRemote: 12 }) }),
      input({
        project: project({
          id: "p-2",
          name: "agentkit",
          slug: "agentkit",
          remote_url: "github.com/org/ak",
        }),
        status: status({ projectId: "p-2", aheadRemote: 9 }),
      }),
    ]);

  it("labels each direction as its own batch", () => {
    expect(bulkLabel(bulkPlan(pushes()), "push")).toBe("Push 2 · ↑21");

    const mixed = deriveSyncRows([
      input({ status: status({ aheadRemote: 12 }) }),
      input({
        project: project({
          id: "p-2",
          name: "agentkit",
          slug: "agentkit",
          remote_url: "github.com/org/ak",
        }),
        status: status({ projectId: "p-2", aheadRemote: 9 }),
      }),
      input({
        project: project({
          id: "p-3",
          name: "webticket-ui",
          slug: "webticket-ui",
          remote_url: "github.com/org/wt",
        }),
        status: status({ projectId: "p-3", behindRemote: 3 }),
      }),
    ]);
    expect(bulkLabel(bulkPlan(mixed), "push")).toBe("Push 2 · ↑21");
    expect(bulkLabel(bulkPlan(mixed), "pull")).toBe("Pull ↓3");
    expect(bulkTargets(mixed, "push").map((r) => r.label)).toEqual(["Agentique", "agentkit"]);
    expect(bulkTargets(mixed, "pull").map((r) => r.label)).toEqual(["webticket-ui"]);
  });

  it("drops the count when a single checkout is in scope", () => {
    const one = deriveSyncRows([input({ status: status({ behindRemote: 3 }) })]);
    expect(bulkLabel(bulkPlan(one), "pull")).toBe("Pull ↓3");
  });

  it("excludes diverged and away checkouts from the plan, and lists them as exceptions", () => {
    const rows = deriveSyncRows([
      input({ status: status({ aheadRemote: 12 }) }),
      input({
        project: project({
          id: "p-2",
          name: "alltix-api",
          slug: "alltix-api",
          remote_url: "github.com/org/ax",
        }),
        status: status({ projectId: "p-2", aheadRemote: 2, behindRemote: 3 }),
      }),
      input({
        project: project({
          id: "p-3",
          name: "agentique",
          slug: "agentique~zbook",
          machineId: "m-z",
        }),
        status: status({ projectId: "p-3", aheadRemote: 4 }),
        machineLabel: "zbook",
        machineOffline: true,
      }),
    ]);
    const plan = bulkPlan(rows);
    expect(plan).toMatchObject({ pushes: 1, pulls: 0, ahead: 12, behind: 0, empty: false });
    expect(exceptionRows(rows).map((r) => r.label)).toEqual(["alltix-api", "agentique"]);
  });

  it("is empty when nothing is mechanical", () => {
    const rows = deriveSyncRows([input({ status: status({ aheadRemote: 2, behindRemote: 3 }) })]);
    expect(bulkPlan(rows).empty).toBe(true);
  });
});
