import { describe, expect, it } from "vitest";
import {
  compareOpenRows,
  type DeriveBadgeInput,
  deriveBadge,
  deriveLivePhrase,
  deriveWorkKind,
  isAwake,
  isAway,
  isHued,
  isStale,
  STALE_AFTER_MS,
  sectionFor,
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
    agentsOut: false,
    ...overrides,
  };
}

describe("deriveBadge", () => {
  it("maps a pending approval to attention", () => {
    expect(deriveBadge(badgeInput({ state: "running", hasPendingApproval: true }))).toBe(
      "attention",
    );
  });

  it("maps a pending question to its own badge", () => {
    expect(deriveBadge(badgeInput({ state: "running", hasPendingQuestion: true }))).toBe(
      "question",
    );
  });

  it("ranks an approval above a question — the approval holds the process", () => {
    expect(
      deriveBadge(
        badgeInput({ state: "running", hasPendingApproval: true, hasPendingQuestion: true }),
      ),
    ).toBe("attention");
  });

  it("keeps attention above planning (plan-review pulses amber)", () => {
    expect(
      deriveBadge(badgeInput({ state: "running", hasPendingApproval: true, isPlanning: true })),
    ).toBe("attention");
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

  it("keeps an idle session working while its subagents are out", () => {
    expect(deriveBadge(badgeInput({ state: "idle", agentsOut: true }))).toBe("working");
    // A dead process has no agents; a stale count must not wake the row.
    expect(deriveBadge(badgeInput({ state: "idle", connected: false, agentsOut: true }))).toBe(
      "off",
    );
    // Attention still outranks liveness.
    expect(
      deriveBadge(badgeInput({ state: "idle", agentsOut: true, hasPendingApproval: true })),
    ).toBe("attention");
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
  // The glyph names the state, so the phrase never repeats it: an approval
  // line is the command itself, not "approve · <command>".
  it("phrases a pending approval as its summary alone", () => {
    expect(
      deriveLivePhrase({ badge: "attention", approvalSummary: "go test -race ./ws/..." }),
    ).toEqual({ text: "go test -race ./ws/...", tone: "attn" });
  });

  it("phrases a pending question as the question itself", () => {
    expect(deriveLivePhrase({ badge: "question", questionSummary: "Which auth method?" })).toEqual({
      text: "Which auth method?",
      tone: "attn",
    });
  });

  it("falls back to one bare word per blocked state", () => {
    expect(deriveLivePhrase({ badge: "attention" })).toEqual({
      text: "needs you",
      tone: "attn",
    });
    expect(deriveLivePhrase({ badge: "question" })).toEqual({
      text: "needs an answer",
      tone: "attn",
    });
  });

  it("uses live narration while working, with a fallback", () => {
    expect(deriveLivePhrase({ badge: "working", liveStatus: "editing AppSidebar.tsx" })).toEqual({
      text: "editing AppSidebar.tsx",
      tone: "work",
    });
    expect(deriveLivePhrase({ badge: "working" })).toEqual({ text: "working", tone: "work" });
  });

  it("phrases planning, merging, and failed", () => {
    expect(deriveLivePhrase({ badge: "planning" })).toEqual({
      text: "planning",
      tone: "work",
    });
    expect(deriveLivePhrase({ badge: "merging" })).toEqual({ text: "merging", tone: "merge" });
    expect(deriveLivePhrase({ badge: "failed", liveStatus: "exit 1" })).toEqual({
      text: "exit 1",
      tone: "fail",
    });
    // Unread has no phrase at all — the NEW pill in the time slot says it,
    // and the row drops to two lines.
    expect(deriveLivePhrase({ badge: "unread" })).toBeNull();
  });

  it("is silent at rest — resting rows have no third line", () => {
    expect(deriveLivePhrase({ badge: null })).toBeNull();
    expect(deriveLivePhrase({ badge: "off" })).toBeNull();
  });
});

describe("deriveWorkKind", () => {
  it("maps the pulse categories to work kinds", () => {
    expect(deriveWorkKind("command")).toBe("run");
    expect(deriveWorkKind("file_write")).toBe("edit");
    expect(deriveWorkKind("file_read")).toBe("read");
    expect(deriveWorkKind("agent")).toBe("delegate");
    expect(deriveWorkKind("mcp")).toBe("tool");
  });

  // A category the frontend has never heard of must not blank the marker.
  it("falls back to generic for unknown and missing categories", () => {
    expect(deriveWorkKind("teleportation")).toBe("generic");
    expect(deriveWorkKind("")).toBe("generic");
    expect(deriveWorkKind(undefined)).toBe("generic");
  });
});

describe("isAwake", () => {
  it("treats every badge except rest, evicted, and unread as awake", () => {
    expect(isAwake("working")).toBe(true);
    expect(isAwake("attention")).toBe(true);
    // A finished session isn't doing anything: no third line.
    expect(isAwake("unread")).toBe(false);
    expect(isAwake(null)).toBe(false);
    expect(isAwake("off")).toBe(false);
  });
});

// Colour answers "is this still mine to deal with", never "is a CLI attached".
// Losing the process is not an outcome: agentique evicts idle CLIs, a restart
// reaps every process group, a crash takes one down — and one message wakes the
// session again in all three cases.
describe("isHued", () => {
  const base = { state: "idle", archived: false, merged: false };

  it("keeps the hue on a session whose process is gone", () => {
    expect(isHued({ ...base, state: "stopped" })).toBe(true);
    expect(isHued({ ...base, state: "failed" })).toBe(true);
    expect(isHued({ ...base, state: "done" })).toBe(true);
  });

  it("greys a session the user filed away", () => {
    expect(isHued({ ...base, archived: true })).toBe(false);
    // Archived outranks everything, including a run still going.
    expect(isHued({ ...base, state: "running", archived: true })).toBe(false);
  });

  it("greys a landed worktree only once the run has ended", () => {
    expect(isHued({ ...base, state: "done", merged: true })).toBe(false);
    // An early merge mid-session is normal — that session is still live work.
    expect(isHued({ ...base, state: "running", merged: true })).toBe(true);
    expect(isHued({ ...base, state: "idle", merged: true })).toBe(true);
  });
});

function makeRow(overrides: Partial<ThreadRowVM> = {}): ThreadRowVM {
  return {
    sessionId: "s-1",
    name: "Row",
    untitled: false,
    depth: 0,
    live: false,
    projectSlug: "proj",
    projectLabel: "proj",
    projectInitials: "PR",
    workspace: "linked",
    parked: false,
    projectColorBg: "#5e9eff",
    projectColorFg: "#5e9eff",
    badge: null,
    restToken: "",
    awake: false,
    hued: true,
    timeLabel: "1h",
    struck: false,
    unread: false,
    pinned: false,
    archived: false,
    canArchive: true,
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

  it("treats a pending question as blocked too", () => {
    const q = makeRow({ sessionId: "q", badge: "question", lastActivity: 100 });
    const fresh = makeRow({ sessionId: "b", badge: "working", lastActivity: 9999 });
    expect([fresh, q].sort(compareOpenRows).map((r) => r.sessionId)).toEqual(["q", "b"]);
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

// The seam this whole change exists for: a cleanly-exited CLI leaves a session
// in a terminal state and nothing more, so the "Finished earlier" shelf is what
// eventually collects it. Before, the runtime stamped completed_at on that same
// transition and the row went straight to the collapsed Archived section.
describe("a cleanly-exited session reaches the shelf, not the archive", () => {
  const now = 1_000_000_000_000;

  it("stays in Open until it has been quiet for a day", () => {
    const justFinished = { state: "done", unread: false, lastActivity: now - 60_000, now };
    expect(isStale(justFinished)).toBe(false);
  });

  it("becomes stale once quiet, exactly like stopped and failed", () => {
    const quiet = (state: string) => ({
      state,
      unread: false,
      lastActivity: now - STALE_AFTER_MS - 1,
      now,
    });
    expect(isStale(quiet("done"))).toBe(true);
    expect(isStale(quiet("stopped"))).toBe(true);
    expect(isStale(quiet("failed"))).toBe(true);
  });

  it("never leaves an unseen outcome on the shelf", () => {
    const unseen = { state: "done", unread: true, lastActivity: now - STALE_AFTER_MS - 1, now };
    expect(isStale(unseen)).toBe(false);
  });
});

// Archiving means "stow this away"; pinning means "keep this at the top". A
// session that claims both is a contradiction the user created by archiving, and
// leaving it in the priority section defeats the point of filing it.
describe("sectionFor", () => {
  const nothing = { archived: false, pinned: false, away: false, stale: false };

  it("files an archived session away even when it is still pinned", () => {
    expect(sectionFor({ ...nothing, archived: true, pinned: true })).toBe("archived");
  });

  it("keeps a pinned, un-archived session at the top", () => {
    expect(sectionFor({ ...nothing, pinned: true })).toBe("pinned");
  });

  // Pinning still outranks the shelf: an old pinned session is there on purpose.
  it("keeps a pinned session out of the shelf", () => {
    expect(sectionFor({ ...nothing, pinned: true, stale: true })).toBe("pinned");
  });

  it("sends a quiet, unpinned session to the shelf", () => {
    expect(sectionFor({ ...nothing, stale: true })).toBe("stale");
  });

  it("leaves everything else open", () => {
    expect(sectionFor(nothing)).toBe("open");
  });

  it("archives regardless of staleness", () => {
    expect(sectionFor({ ...nothing, archived: true, stale: true })).toBe("archived");
  });

  it("shelves an unreachable session under Away", () => {
    expect(sectionFor({ ...nothing, away: true })).toBe("away");
  });

  // A closed laptop does not get to undo a pin, and it does not get to
  // un-archive either — both are gestures, where away is a passing fact.
  it("lets a pin and an archive outrank away", () => {
    expect(sectionFor({ ...nothing, away: true, pinned: true })).toBe("pinned");
    expect(sectionFor({ ...nothing, away: true, archived: true })).toBe("archived");
  });

  // "Finished earlier" carries an Archive-all that would fail on every row
  // whose machine is gone, so the shelf that explains itself wins.
  it("puts away above the finished shelf", () => {
    expect(sectionFor({ ...nothing, away: true, stale: true })).toBe("away");
  });
});

// The row is not one you have not dealt with — it is one you *cannot* deal
// with: pin, archive and open all route to the machine that owns the session.
describe("isAway", () => {
  const base = { machineOffline: true, blocked: false, unread: false, active: false };

  it("shelves a session whose machine is unreachable", () => {
    expect(isAway(base)).toBe(true);
  });

  it("leaves a reachable session alone", () => {
    expect(isAway({ ...base, machineOffline: false })).toBe(false);
  });

  // Amber is the one thing the sidebar never hides, even when answering it has
  // to wait for the machine to come back.
  it("never files a row that is blocked on a human", () => {
    expect(isAway({ ...base, blocked: true })).toBe(false);
  });

  it("keeps an unseen outcome visible, exactly like the finished shelf", () => {
    expect(isAway({ ...base, unread: true })).toBe(false);
  });

  // The filing lands the instant a machine drops, so the row you are reading
  // must not fold itself into a collapsed shelf underneath you.
  it("never files the session that is open right now", () => {
    expect(isAway({ ...base, active: true })).toBe(false);
  });
});
