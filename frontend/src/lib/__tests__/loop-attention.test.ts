import { describe, expect, it } from "vitest";
import type { ScheduleInfo } from "~/lib/generated-types";
import { loopBadgeState } from "~/lib/loop-attention";

function schedule(overrides: Partial<ScheduleInfo> = {}): ScheduleInfo {
  return {
    id: "sc_1",
    projectId: "p",
    sessionId: "s",
    name: "nightly sweep",
    prompt: "",
    cron: "",
    mode: "",
    enabled: true,
    pauseReason: "",
    attention: "",
    attentionRunId: "",
    nextRunAt: "",
    expiresAt: "",
    lastRunAt: "",
    lastViewedAt: "",
    consecutiveFailures: 0,
    createdBy: "",
    createdAt: "",
    updatedAt: "",
    ...overrides,
  };
}

describe("loopBadgeState", () => {
  it("says nothing when every loop is healthy", () => {
    expect(loopBadgeState([schedule(), schedule({ id: "sc_2" })])).toBeNull();
  });

  it("says nothing for a session with no loops", () => {
    expect(loopBadgeState([])).toBeNull();
  });

  it("raises a loop paused after repeated failures", () => {
    expect(loopBadgeState([schedule({ attention: "failed" })])).toEqual({
      kind: "paused",
      count: 1,
    });
  });

  it("treats a run needing a look as waiting on you", () => {
    expect(loopBadgeState([schedule({ attention: "action_needed" })])).toEqual({
      kind: "blocked",
      count: 1,
    });
  });

  it("treats a schedule parked for approval as waiting on you", () => {
    expect(loopBadgeState([schedule({ pauseReason: "pending-approval" })])).toEqual({
      kind: "blocked",
      count: 1,
    });
  });

  it("counts every loop in the state it reports", () => {
    expect(
      loopBadgeState([
        schedule({ id: "a", attention: "failed" }),
        schedule({ id: "b", attention: "failed" }),
        schedule({ id: "c" }),
      ]),
    ).toEqual({ kind: "paused", count: 2 });
  });

  it("ranks waiting-on-you above paused, matching the app's state priority", () => {
    expect(
      loopBadgeState([
        schedule({ id: "a", attention: "failed" }),
        schedule({ id: "b", attention: "action_needed" }),
      ]),
    ).toEqual({ kind: "blocked", count: 1 });
  });

  it("counts a loop once even when the pause and the attention agree", () => {
    expect(
      loopBadgeState([schedule({ pauseReason: "pending-approval", attention: "action_needed" })]),
    ).toEqual({ kind: "blocked", count: 1 });
  });

  it("ignores a pause that is not the user's to resolve", () => {
    // Only "pending-approval" is blocked on a human; other pause reasons are
    // reported through `attention`, if at all.
    expect(loopBadgeState([schedule({ enabled: false, pauseReason: "completed" })])).toBeNull();
  });
});
