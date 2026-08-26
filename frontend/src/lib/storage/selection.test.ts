import { describe, expect, it } from "vitest";
import type { SessionStorage } from "~/lib/generated-types";
import { canDelete, canReclaim, freedBytes, reconcile, summarize } from "~/lib/storage/selection";

function session(over: Partial<SessionStorage> = {}): SessionStorage {
  return {
    sessionId: "s1",
    name: "Session",
    state: "stopped",
    worktreePath: "/data/worktrees/proj/session-s1",
    bytes: 100,
    updatedAt: "2026-08-26T00:00:00Z",
    archivedAt: "",
    archived: false,
    merged: false,
    orphaned: false,
    tempBytes: 50,
    totalBytes: 150,
    reclaimable: true,
    safety: "safe",
    safetyReason: "",
    ...over,
  };
}

describe("canDelete", () => {
  it("accepts only the server's positive claim", () => {
    expect(canDelete(session({ safety: "safe" }))).toBe(true);
    expect(canDelete(session({ safety: "ahead" }))).toBe(false);
    expect(canDelete(session({ safety: "dirty" }))).toBe(false);
    expect(canDelete(session({ safety: "unknown" }))).toBe(false);
  });

  // A peer that predates the field sends nothing. "Not reported" is not "safe":
  // reading a missing field as permission is how a bulk destructive action ends
  // up acting on work nobody checked.
  it("treats an absent verdict as not established", () => {
    const older = session();
    delete (older as Partial<SessionStorage>).safety;
    expect(canDelete(older)).toBe(false);
  });

  // The whole point of the change: the flag says agentique did not do the
  // merge, git says the commits are already on HEAD, and delete follows git.
  it("does not require the merged flag", () => {
    expect(canDelete(session({ merged: false, safety: "safe" }))).toBe(true);
  });
});

describe("canReclaim", () => {
  it("follows the server's flag", () => {
    expect(canReclaim(session({ reclaimable: true }))).toBe(true);
    expect(canReclaim(session({ reclaimable: false }))).toBe(false);
  });

  it("treats an absent flag as not offered", () => {
    const older = session();
    delete (older as Partial<SessionStorage>).reclaimable;
    expect(canReclaim(older)).toBe(false);
  });

  // Archiving is a filing gesture, not a safety claim — but the reversible verb
  // does not need a safety claim, which is the whole reason it exists.
  it("is offered on an archived session with unmerged commits", () => {
    const archived = session({ archived: true, reclaimable: true, safety: "ahead" });
    expect(canReclaim(archived)).toBe(true);
    expect(canDelete(archived)).toBe(false);
  });
});

describe("freedBytes", () => {
  it("counts the temp artifacts a reclaim also removes", () => {
    expect(freedBytes(session({ bytes: 100, tempBytes: 50, totalBytes: 150 }))).toBe(150);
  });

  it("falls back to the worktree size when a peer sends no total", () => {
    const older = session({ bytes: 100 });
    delete (older as Partial<SessionStorage>).totalBytes;
    expect(freedBytes(older)).toBe(100);
  });
});

describe("summarize", () => {
  it("is empty for an empty selection", () => {
    const s = summarize([]);
    expect(s.count).toBe(0);
    expect(s.bytes).toBe(0);
    expect(s.deleteBlockedReason).toBe("");
    expect(s.reclaimBlockedReason).toBe("");
  });

  it("reports no blocker when every row clears both bars", () => {
    const s = summarize([session({ sessionId: "a" }), session({ sessionId: "b" })]);
    expect(s.count).toBe(2);
    expect(s.bytes).toBe(300);
    expect(s.deletable).toHaveLength(2);
    expect(s.reclaimable).toHaveLength(2);
    expect(s.deleteBlockedReason).toBe("");
  });

  // A count, not a list: naming one blocker in the bar makes the others
  // invisible, and the rows carry the individual reasons.
  it("counts a partial blocker rather than naming one", () => {
    const s = summarize([
      session({ sessionId: "a" }),
      session({ sessionId: "b", safety: "ahead", safetyReason: "has commits…" }),
      session({ sessionId: "c", safety: "ahead", safetyReason: "has commits…" }),
    ]);
    expect(s.deleteBlockedReason).toBe("2 of 3 are not eligible");
    expect(s.deletable).toHaveLength(1);
    // Reclaim is unaffected — being ahead does not block the reversible verb.
    expect(s.reclaimBlockedReason).toBe("");
  });

  it("says so when nothing in the selection is eligible", () => {
    const s = summarize([
      session({ sessionId: "a", safety: "live", reclaimable: false }),
      session({ sessionId: "b", safety: "live", reclaimable: false }),
    ]);
    expect(s.deleteBlockedReason).toBe("None of these are eligible");
    expect(s.reclaimBlockedReason).toBe("None of these are eligible");
  });

  it("uses the singular for a selection of one", () => {
    const s = summarize([session({ safety: "dirty", reclaimable: false })]);
    expect(s.deleteBlockedReason).toBe("This session is not eligible");
  });
});

describe("reconcile", () => {
  // The walk is cached for a minute and refreshed after every action, so a
  // selection routinely outlives the rows it was made from.
  it("drops ids that are no longer on the page", () => {
    const selected = new Set(["a", "b", "gone"]);
    const next = reconcile(selected, [session({ sessionId: "a" }), session({ sessionId: "b" })]);
    expect([...next].sort()).toEqual(["a", "b"]);
  });

  it("empties a selection when every row vanishes", () => {
    expect(reconcile(new Set(["a"]), []).size).toBe(0);
  });
});
