import { describe, expect, it } from "vitest";
import type { SessionStorage, StorageUsage } from "~/lib/generated-types";
import { buildBreakdown } from "./breakdown";

const session = (over: Partial<SessionStorage>): SessionStorage => ({
  sessionId: "s",
  name: "s",
  state: "stopped",
  worktreePath: "/w/s",
  bytes: 0,
  updatedAt: "",
  archivedAt: "",
  archived: false,
  merged: false,
  orphaned: false,
  tempBytes: 0,
  totalBytes: 0,
  reclaimable: false,
  ...over,
});

/** Worktrees 1000 = live 300 + reclaimable 600 + orphan 100. */
const usage = (over: Partial<StorageUsage> = {}): StorageUsage => ({
  computedAt: "",
  disk: { path: "/", totalBytes: 0, freeBytes: 0, usedBytes: 0, usagePercent: 0 },
  dataDirBytes: 1000,
  categories: [
    { key: "worktrees", label: "Worktrees", bytes: 1000 },
    { key: "database", label: "Database", bytes: 50 },
    { key: "backups", label: "Backups", bytes: 200 },
    { key: "certs", label: "Certificates", bytes: 0 },
  ],
  tempCategories: [
    { key: "chrome-profiles", label: "Browser profiles", bytes: 70 },
    { key: "scratchpads", label: "Agent scratchpads", bytes: 30 },
  ],
  tempBytes: 100,
  projects: [
    {
      projectId: "p",
      name: "P",
      slug: "p",
      color: "",
      icon: "",
      totalBytes: 900,
      sessions: [
        session({ sessionId: "live", bytes: 300, totalBytes: 300, reclaimable: false }),
        session({ sessionId: "a", bytes: 400, tempBytes: 60, totalBytes: 460, reclaimable: true }),
        session({ sessionId: "b", bytes: 200, tempBytes: 40, totalBytes: 240, reclaimable: true }),
      ],
    },
  ],
  orphans: [session({ sessionId: "", orphaned: true, bytes: 100, totalBytes: 100 })],
  ...over,
});

const row = (u: StorageUsage, key: string) => buildBreakdown(u).rows.find((r) => r.key === key);

describe("buildBreakdown", () => {
  // The whole point of splitting worktrees is that the split is exact rather
  // than estimated. If these three stop summing to the backend's category
  // total, some bytes are being reported twice or not at all.
  it("splits the worktrees category into three parts that add back up", () => {
    const b = buildBreakdown(usage());
    const live = b.rows.find((r) => r.key === "worktrees-live")?.bytes ?? 0;
    const finished = b.rows.find((r) => r.key === "worktrees-finished")?.bytes ?? 0;
    const orphaned = b.rows.find((r) => r.key === "worktrees-orphaned")?.bytes ?? 0;

    expect(live).toBe(300);
    expect(finished).toBe(600);
    expect(orphaned).toBe(100);
    expect(live + finished + orphaned).toBe(1000);
  });

  // reclaimableBytes on the wire is worktree + temp, so reading it into the
  // worktree row would count the temp categories twice.
  it("counts only worktree bytes in the finished row, never their temp", () => {
    expect(row(usage(), "worktrees-finished")?.bytes).toBe(600);
    expect(row(usage(), "chrome-profiles")?.bytes).toBe(70);
    expect(row(usage(), "scratchpads")?.bytes).toBe(30);
  });

  it("reports the sweep in full — worktree plus temp — for the button's copy", () => {
    expect(buildBreakdown(usage()).sweepBytes).toBe(700);
  });

  it("totals every row it drew, so the bars share one denominator", () => {
    const b = buildBreakdown(usage());
    expect(b.total).toBe(b.rows.reduce((a, r) => a + r.bytes, 0));
    expect(b.total).toBe(1000 + 50 + 200 + 100);
  });

  it("drops empty categories rather than drawing a bar at zero", () => {
    expect(row(usage(), "certs")).toBeUndefined();
    expect(row(usage(), "session-files")).toBeUndefined();
  });

  it("orders by what can be done about it, then by size", () => {
    const classes = buildBreakdown(usage()).rows.map((r) => r.cls);
    expect(classes).toEqual([...classes].sort(rank));
  });

  // The verb is offered only where the server has something to act on.
  it("offers Reclaim only on the finished row, and only when there is one", () => {
    const withWork = buildBreakdown(usage()).rows.filter((r) => r.action === "reclaim");
    expect(withWork.map((r) => r.key)).toEqual(["worktrees-finished"]);

    const idle = usage({
      projects: [
        {
          projectId: "p",
          name: "P",
          slug: "p",
          color: "",
          icon: "",
          totalBytes: 300,
          sessions: [session({ sessionId: "live", bytes: 1000, reclaimable: false })],
        },
      ],
    });
    expect(buildBreakdown(idle).rows.some((r) => r.action)).toBe(false);
  });

  // An older peer sends neither `reclaimable` nor the temp categories. Nothing
  // may be offered on the strength of an absent field.
  it("treats a peer that never spoke reclaimable as having nothing to sweep", () => {
    const legacy = usage({
      tempCategories: undefined,
      tempBytes: undefined,
      projects: [
        {
          projectId: "p",
          name: "P",
          slug: "p",
          color: "",
          icon: "",
          totalBytes: 900,
          sessions: [session({ sessionId: "a", bytes: 900, reclaimable: undefined })],
        },
      ],
      orphans: [],
    });
    const b = buildBreakdown(legacy);
    expect(b.rows.some((r) => r.action)).toBe(false);
    expect(b.sweepBytes).toBe(0);
    expect(b.rows.find((r) => r.key === "worktrees-live")?.bytes).toBe(1000);
  });

  it("never reports a negative bar when the walk and the rows disagree", () => {
    // A session sized after the category walk can overshoot it; clamping keeps
    // the live row at zero rather than drawing a bar backwards.
    const skewed = usage({
      categories: [{ key: "worktrees", label: "Worktrees", bytes: 100 }],
    });
    expect(row(skewed, "worktrees-live")).toBeUndefined();
    expect(buildBreakdown(skewed).rows.every((r) => r.bytes >= 0)).toBe(true);
  });
});

const ORDER = { live: 0, sweep: 1, policy: 2 } as const;
const rank = (a: keyof typeof ORDER, b: keyof typeof ORDER) => ORDER[a] - ORDER[b];
