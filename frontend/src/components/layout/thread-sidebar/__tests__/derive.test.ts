import { describe, expect, it } from "vitest";
import {
  compareOpenRows,
  type DeriveBadgeInput,
  type DeriveMachineLineInput,
  deriveBadge,
  deriveMachineLine,
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

function lineInput(overrides: Partial<DeriveMachineLineInput> = {}): DeriveMachineLineInput {
  return { state: "idle", badge: null, merged: false, ...overrides };
}

describe("deriveMachineLine", () => {
  it("phrases a pending approval with its summary", () => {
    expect(
      deriveMachineLine(
        lineInput({ badge: "attention", approvalSummary: "go test -race ./ws/..." }),
      ),
    ).toEqual({ text: "approve · go test -race ./ws/...", tone: "attn" });
  });

  it("falls back to a generic attention phrase without a summary", () => {
    expect(deriveMachineLine(lineInput({ badge: "attention" }))).toEqual({
      text: "needs your input",
      tone: "attn",
    });
  });

  it("uses live narration while working, with a fallback", () => {
    expect(
      deriveMachineLine(
        lineInput({ state: "running", badge: "working", liveStatus: "editing AppSidebar.tsx" }),
      ),
    ).toEqual({ text: "editing AppSidebar.tsx", tone: "work" });
    expect(deriveMachineLine(lineInput({ state: "running", badge: "working" }))).toEqual({
      text: "working…",
      tone: "work",
    });
  });

  it("phrases planning, merging, failed, unread, draft, and off", () => {
    expect(deriveMachineLine(lineInput({ badge: "planning" }))).toEqual({
      text: "drafting a plan",
      tone: "work",
    });
    expect(deriveMachineLine(lineInput({ badge: "merging" }))).toEqual({
      text: "merging…",
      tone: "merge",
    });
    expect(deriveMachineLine(lineInput({ badge: "failed", liveStatus: "exit 1" }))).toEqual({
      text: "exit 1",
      tone: "fail",
    });
    expect(deriveMachineLine(lineInput({ badge: "unread" }))).toEqual({
      text: "finished — unread",
      tone: "unread",
    });
    expect(deriveMachineLine(lineInput({ badge: "draft" }))).toEqual({
      text: "draft — not sent",
      tone: "draft",
    });
    expect(deriveMachineLine(lineInput({ badge: "off" }))).toEqual({
      text: "resumes on next message",
      tone: "muted",
    });
  });

  it("shows outcome over branch at rest", () => {
    expect(deriveMachineLine(lineInput({ merged: true, branch: "session-5f2209dd" }))).toEqual({
      text: "merged",
      tone: "muted",
    });
    expect(deriveMachineLine(lineInput({ state: "stopped", branch: "session-5f2209dd" }))).toEqual({
      text: "stopped by you",
      tone: "muted",
    });
  });

  it("falls back to branch, then archived, then empty at rest", () => {
    expect(deriveMachineLine(lineInput({ branch: "session-5f2209dd" }))).toEqual({
      text: "session-5f2209dd",
      tone: "muted",
    });
    expect(deriveMachineLine(lineInput({ completedAt: "2026-08-01T00:00:00Z" }))).toEqual({
      text: "archived",
      tone: "muted",
    });
    expect(deriveMachineLine(lineInput())).toEqual({ text: "", tone: "muted" });
  });

  it("prefixes the machine label for remote sessions", () => {
    expect(
      deriveMachineLine(lineInput({ branch: "session-5f2209dd", remoteMachineLabel: "epsilon" })),
    ).toEqual({ text: "on epsilon · session-5f2209dd", tone: "muted" });
    // Tone follows the body, not the prefix.
    expect(
      deriveMachineLine(
        lineInput({
          badge: "attention",
          approvalSummary: "rm -rf dist",
          remoteMachineLabel: "epsilon",
        }),
      ),
    ).toEqual({ text: "on epsilon · approve · rm -rf dist", tone: "attn" });
  });

  it("shows just the machine when there is nothing else to say", () => {
    expect(deriveMachineLine(lineInput({ remoteMachineLabel: "epsilon" }))).toEqual({
      text: "on epsilon",
      tone: "muted",
    });
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
    machineLine: { text: "", tone: "muted" },
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
