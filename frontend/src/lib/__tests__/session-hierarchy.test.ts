import { describe, expect, it } from "vitest";
import { buildSessionHierarchy } from "~/lib/session-hierarchy";
import type { SessionMetadata } from "~/stores/chat-types";

function session(id: string, name: string, parent?: string): { meta: SessionMetadata } {
  return {
    meta: {
      id,
      name,
      projectId: "p1",
      state: "idle",
      connected: true,
      model: "opus",
      permissionMode: "default",
      autoApproveMode: "manual",
      totalCost: 0,
      turnCount: 0,
      commitsAhead: 0,
      commitsBehind: 0,
      gitVersion: 0,
      behaviorPresets: {
        autoCommit: false,
        suggestParallel: false,
        planFirst: false,
        terse: false,
      },
      createdAt: "2026-04-22T00:00:00Z",
      updatedAt: "2026-04-22T00:00:00Z",
      parentSessionId: parent,
    } as unknown as SessionMetadata,
  };
}

describe("buildSessionHierarchy", () => {
  it("returns empty array when no parent relationships exist", () => {
    const sessions = { a: session("a", "A"), b: session("b", "B") };
    expect(buildSessionHierarchy(sessions)).toEqual([]);
  });

  it("builds a single lead → workers tree", () => {
    const sessions = {
      lead: session("lead", "Lead"),
      w1: session("w1", "W1", "lead"),
      w2: session("w2", "W2", "lead"),
    };
    const tree = buildSessionHierarchy(sessions);
    expect(tree).toHaveLength(1);
    expect(tree[0]?.session.id).toBe("lead");
    expect(tree[0]?.children.map((c) => c.session.id).sort()).toEqual(["w1", "w2"]);
  });

  it("handles grandchildren (multi-level trees)", () => {
    const sessions = {
      lead: session("lead", "Lead"),
      mid: session("mid", "Mid", "lead"),
      leaf: session("leaf", "Leaf", "mid"),
    };
    const tree = buildSessionHierarchy(sessions);
    expect(tree).toHaveLength(1);
    expect(tree[0]?.children[0]?.children[0]?.session.id).toBe("leaf");
  });

  it("treats sessions with dangling parent pointers as roots (drops them if childless)", () => {
    const sessions = {
      orphan: session("orphan", "Orphan", "ghost-lead"),
    };
    // Orphan has no children, so it's dropped from the top-level tree.
    expect(buildSessionHierarchy(sessions)).toEqual([]);
  });

  it("includes dangling-parent roots when they have children of their own", () => {
    const sessions = {
      orphan: session("orphan", "Orphan", "ghost"),
      sub: session("sub", "Sub", "orphan"),
    };
    const tree = buildSessionHierarchy(sessions);
    expect(tree).toHaveLength(1);
    expect(tree[0]?.session.id).toBe("orphan");
    expect(tree[0]?.children.map((c) => c.session.id)).toEqual(["sub"]);
  });

  it("sorts roots alphabetically by name", () => {
    const sessions = {
      beta: session("beta", "Beta"),
      alpha: session("alpha", "Alpha"),
      bw: session("bw", "BetaWorker", "beta"),
      aw: session("aw", "AlphaWorker", "alpha"),
    };
    const tree = buildSessionHierarchy(sessions);
    expect(tree.map((n) => n.session.name)).toEqual(["Alpha", "Beta"]);
  });
});
