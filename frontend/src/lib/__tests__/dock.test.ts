import { describe, expect, it } from "vitest";
import type { AgentBadgeState } from "~/lib/agent-runs";
import type { LoopBadgeState } from "~/lib/loop-attention";
import {
  availableDockViews,
  type DockAvailability,
  dockAlertState,
  legacyTabToDock,
  resolveDockView,
} from "~/lib/session/dock";

const NOTHING: DockAvailability = {
  work: false,
  changes: false,
  loops: false,
  browser: false,
};
const quiet: AgentBadgeState = { running: 0, failed: 0 };

describe("availableDockViews", () => {
  it("keeps the declared order regardless of which views exist", () => {
    expect(availableDockViews({ work: false, changes: true, loops: false, browser: true })).toEqual(
      ["changes", "browser"],
    );
  });

  it("is empty when the session has nothing to dock", () => {
    expect(availableDockViews(NOTHING)).toEqual([]);
  });
});

describe("resolveDockView", () => {
  it("keeps a stored view that is still available", () => {
    expect(resolveDockView("loops", { ...NOTHING, work: true, loops: true })).toBe("loops");
  });

  it("falls back rather than collapsing when the view's subject is gone", () => {
    // The diff was merged away while the dock was open on it. Closing would
    // read as the user's own gesture, so the dock stays open on what is left.
    expect(resolveDockView("changes", { ...NOTHING, work: true })).toBe("work");
  });

  it("falls back in declared order, not to the first thing that happens to exist", () => {
    expect(resolveDockView("changes", { ...NOTHING, browser: true, loops: true })).toBe("loops");
  });

  it("returns null only when there is nothing at all to show", () => {
    expect(resolveDockView("work", NOTHING)).toBeNull();
    expect(resolveDockView(null, NOTHING)).toBeNull();
  });

  it("treats an absent stored view as no preference", () => {
    expect(resolveDockView(undefined, { ...NOTHING, changes: true })).toBe("changes");
  });
});

describe("legacyTabToDock", () => {
  it("maps both agent-era tabs into the group that absorbed them", () => {
    expect(legacyTabToDock("todos")).toBe("work");
    expect(legacyTabToDock("agents")).toBe("work");
  });

  it("still understands the pre-rename git tab", () => {
    expect(legacyTabToDock("git")).toBe("changes");
    expect(legacyTabToDock("changes")).toBe("changes");
  });

  it("maps chat to nothing — chat is the page now, not a view", () => {
    expect(legacyTabToDock("chat")).toBeNull();
  });

  it("ignores values it does not know", () => {
    expect(legacyTabToDock(undefined)).toBeNull();
    expect(legacyTabToDock("nonsense")).toBeNull();
  });
});

describe("dockAlertState", () => {
  const blocked: LoopBadgeState = { kind: "blocked", count: 2 };
  const paused: LoopBadgeState = { kind: "paused", count: 1 };

  it("says nothing when nothing needs the user", () => {
    expect(dockAlertState(quiet, null)).toBeNull();
  });

  it("ranks waiting-on-you above a failure", () => {
    expect(dockAlertState({ running: 0, failed: 3 }, blocked)).toEqual({
      kind: "blocked",
      count: 2,
    });
  });

  it("ranks a failure above something merely live", () => {
    expect(dockAlertState({ running: 4, failed: 1 }, null)).toEqual({ kind: "failed", count: 1 });
  });

  it("treats a paused loop as a failure, below an agent failure", () => {
    expect(dockAlertState({ running: 0, failed: 2 }, paused)).toEqual({
      kind: "failed",
      count: 2,
    });
    expect(dockAlertState(quiet, paused)).toEqual({ kind: "failed", count: 1 });
  });

  it("reports live agents when nothing worse is happening", () => {
    expect(dockAlertState({ running: 2, failed: 0 }, null)).toEqual({ kind: "live", count: 2 });
  });
});
