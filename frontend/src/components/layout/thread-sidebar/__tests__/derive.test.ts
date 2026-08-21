import { describe, expect, it } from "vitest";
import {
  compareOpenRows,
  type DeriveBadgeInput,
  deriveBadge,
  deriveLivePhrase,
  deriveRestToken,
  isAwake,
  isStale,
  STALE_AFTER_MS,
} from "../derive";
import type { ThreadRowVM } from "../types";

function badgeInput(overrides: Partial<DeriveBadgeInput> = {}): DeriveBadgeInput {
  return {
    state: "idle",
    hasPendingApproval: false,
    hasPendingQuestion: false,
    isPlanning: false,
    hasUnseenCompletion: false,
    connected: true,
    ...overrides,
  };
}

describe("deriveBadge", () => {
  it("maps a pending approval to attention", () => {
    expect(deriveBadge(badgeInput({ state: "running", hasPendingApproval: true }))).toBe(
      "attention",
    );
  });

  it("maps a pending question to attention", () => {
    expect(deriveBadge(badgeInput({ state: "running", hasPendingQuestion: true }))).toBe(
      "attention",
    );
  });

  it("keeps attention above planning (plan-review pulses amber)", () => {
    expect(
      deriveBadge(badgeInput({ state: "running", hasPendingApproval: true, isPlanning: true })),
    ).toBe("attention");
  });

  it("maps drafts before live state", () => {
    expect(deriveBadge(badgeInput({ isDraft: true }))).toBe("draft");
  });

  it("maps running to working, and planning while running to planning", () => {
    expect(deriveBadge(badgeInput({ state: "running" }))).toBe("working");
    expect(deriveBadge(badgeInput({ state: "running", isPlanning: true }))).toBe("planning");
  });

  it("maps merging and failed to their own badges", () => {
    expect(deriveBadge(badgeInput({ state: "merging" }))).toBe("merging");
    expect(deriveBadge(badgeInput({ state: "failed" }))).toBe("failed");
  });

  it("ranks failed above unseen completion", () => {
    expect(deriveBadge(badgeInput({ state: "failed", hasUnseenCompletion: true }))).toBe("failed");
  });

  it("maps an unseen completion to unread", () => {
    expect(deriveBadge(badgeInput({ state: "idle", hasUnseenCompletion: true }))).toBe("unread");
  });

  it("marks only disconnected idle sessions as off", () => {
    expect(deriveBadge(badgeInput({ state: "idle", connected: false }))).toBe("off");
    expect(deriveBadge(badgeInput({ state: "running", connected: false }))).toBe("working");
  });

  it("shows no badge at rest", () => {
    expect(deriveBadge(badgeInput({ state: "idle" }))).toBeNull();
    expect(deriveBadge(badgeInput({ state: "done" }))).toBeNull();
    expect(deriveBadge(badgeInput({ state: "stopped" }))).toBeNull();
  });
});

describe("deriveLivePhrase", () => {
  it("phrases a pending approval with its summary", () => {
    expect(
      deriveLivePhrase({ badge: "attention", approvalSummary: "go test -race ./ws/..." }),
    ).toEqual({ text: "approve · go test -race ./ws/...", tone: "attn" });
  });

  it("falls back to a generic attention phrase without a summary", () => {
    expect(deriveLivePhrase({ badge: "attention" })).toEqual({
      text: "needs your input",
      tone: "attn",
    });
  });

  it("uses live narration while working, with a fallback", () => {
    expect(deriveLivePhrase({ badge: "working", liveStatus: "editing AppSidebar.tsx" })).toEqual({
      text: "editing AppSidebar.tsx",
      tone: "work",
    });
    expect(deriveLivePhrase({ badge: "working" })).toEqual({ text: "working…", tone: "work" });
  });

  it("phrases planning, merging, failed, unread, and draft", () => {
    expect(deriveLivePhrase({ badge: "planning" })).toEqual({
      text: "drafting a plan",
      tone: "work",
    });
    expect(deriveLivePhrase({ badge: "merging" })).toEqual({ text: "merging…", tone: "merge" });
    expect(deriveLivePhrase({ badge: "failed", liveStatus: "exit 1" })).toEqual({
      text: "exit 1",
      tone: "fail",
    });
    expect(deriveLivePhrase({ badge: "unread" })).toEqual({
      text: "finished — unread",
      tone: "unread",
    });
    expect(deriveLivePhrase({ badge: "draft" })).toEqual({
      text: "draft — not sent",
      tone: "draft",
    });
  });

  it("is silent at rest — resting rows have no third line", () => {
    expect(deriveLivePhrase({ badge: null })).toBeNull();
    expect(deriveLivePhrase({ badge: "off" })).toBeNull();
  });
});

describe("isAwake", () => {
  it("treats every badge except rest and evicted as awake", () => {
    expect(isAwake("working")).toBe(true);
    expect(isAwake("attention")).toBe(true);
    expect(isAwake("unread")).toBe(true);
    expect(isAwake(null)).toBe(false);
    expect(isAwake("off")).toBe(false);
  });
});

describe("deriveRestToken", () => {
  it("ranks merged over stopped over done", () => {
    expect(deriveRestToken({ state: "stopped", merged: true, connected: true })).toBe("merged");
    expect(deriveRestToken({ state: "stopped", merged: false, connected: true })).toBe("stopped");
    expect(deriveRestToken({ state: "done", merged: false, connected: true })).toBe("done");
  });

  it("marks a disconnected idle session as evicted", () => {
    expect(deriveRestToken({ state: "idle", merged: false, connected: false })).toBe("evicted");
  });

  it("says nothing for a connected idle session", () => {
    expect(deriveRestToken({ state: "idle", merged: false, connected: true })).toBe("");
  });
});

function makeRow(overrides: Partial<ThreadRowVM> = {}): ThreadRowVM {
  return {
    sessionId: "s-1",
    name: "Row",
    untitled: false,
    projectSlug: "proj",
    projectInitials: "PR",
    projectColorBg: "#5e9eff",
    projectColorFg: "#5e9eff",
    badge: null,
    restToken: "",
    awake: false,
    timeLabel: "1h",
    struck: false,
    unread: false,
    pinned: false,
    lastActivity: 0,
    ...overrides,
  };
}

describe("compareOpenRows", () => {
  it("puts attention rows before everything, regardless of recency", () => {
    const attn = makeRow({ sessionId: "a", badge: "attention", lastActivity: 100 });
    const fresh = makeRow({ sessionId: "b", badge: "working", lastActivity: 9999 });
    expect([fresh, attn].sort(compareOpenRows).map((r) => r.sessionId)).toEqual(["a", "b"]);
  });

  it("orders non-attention rows by last activity, newest first", () => {
    const old = makeRow({ sessionId: "old", lastActivity: 100 });
    const mid = makeRow({ sessionId: "mid", badge: "working", lastActivity: 500 });
    const fresh = makeRow({ sessionId: "new", badge: "unread", lastActivity: 900 });
    expect([old, mid, fresh].sort(compareOpenRows).map((r) => r.sessionId)).toEqual([
      "new",
      "mid",
      "old",
    ]);
  });

  it("orders attention rows among themselves by recency", () => {
    const a = makeRow({ sessionId: "a", badge: "attention", lastActivity: 100 });
    const b = makeRow({ sessionId: "b", badge: "attention", lastActivity: 200 });
    expect([a, b].sort(compareOpenRows).map((r) => r.sessionId)).toEqual(["b", "a"]);
  });

  it("breaks exact ties by sessionId for a stable order", () => {
    const a = makeRow({ sessionId: "a", lastActivity: 100 });
    const b = makeRow({ sessionId: "b", lastActivity: 100 });
    expect([b, a].sort(compareOpenRows).map((r) => r.sessionId)).toEqual(["a", "b"]);
  });
});

describe("isStale", () => {
  const DAY = STALE_AFTER_MS;
  const base = { state: "stopped", unread: false, lastActivity: 0, now: DAY + 1 };

  it("collects a terminal, seen session after a quiet day", () => {
    expect(isStale(base)).toBe(true);
    expect(isStale({ ...base, state: "done" })).toBe(true);
    expect(isStale({ ...base, state: "failed" })).toBe(true);
  });

  it("never collects non-terminal sessions — merge alone is not end-of-life", () => {
    // An early-merged session that keeps working stays idle/running, not terminal.
    expect(isStale({ ...base, state: "idle" })).toBe(false);
    expect(isStale({ ...base, state: "running" })).toBe(false);
  });

  it("keeps unread outcomes visible regardless of age", () => {
    expect(isStale({ ...base, unread: true })).toBe(false);
  });

  it("waits out the quiet period", () => {
    expect(isStale({ ...base, now: DAY - 1 })).toBe(false);
    expect(isStale({ ...base, now: DAY + 1 })).toBe(true);
  });
});
