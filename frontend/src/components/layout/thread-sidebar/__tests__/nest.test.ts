import { describe, expect, it } from "vitest";
import { nestWorkers } from "../nest";
import type { ThreadGroups, ThreadRowVM } from "../types";

function row(sessionId: string, overrides: Partial<ThreadRowVM> = {}): ThreadRowVM {
  return {
    sessionId,
    name: sessionId,
    untitled: false,
    depth: 0,
    live: false,
    projectSlug: "proj",
    projectLabel: "proj",
    projectInitials: "PR",
    workspace: "linked",
    projectColorBg: "#73daca",
    projectColorFg: "#73daca",
    badge: null,
    awake: true,
    hued: true,
    restToken: "",
    parked: false,
    timeLabel: "1m",
    struck: false,
    unread: false,
    pinned: false,
    archived: false,
    canArchive: true,
    lastActivity: 0,
    ...overrides,
  };
}

function groups(over: Partial<ThreadGroups> = {}): ThreadGroups {
  return { pinned: [], open: [], stale: [], archived: [], ...over };
}

const NONE: ReadonlySet<string> = new Set<string>();

describe("nestWorkers", () => {
  it("places workers under their lead, newest first, marking the last", () => {
    const result = nestWorkers(
      groups({
        open: [
          row("lead", { workers: 2 }),
          row("w-old", { parentSessionId: "lead", lastActivity: 10 }),
          row("w-new", { parentSessionId: "lead", lastActivity: 20 }),
        ],
      }),
      NONE,
    );

    expect(result.open.map((r) => r.sessionId)).toEqual(["lead", "w-new", "w-old"]);
    expect(result.open.map((r) => r.depth)).toEqual([0, 1, 1]);
    expect(result.open[1]?.lastChild).toBeFalsy();
    expect(result.open[2]?.lastChild).toBe(true);
  });

  it("pulls a worker into its lead's section rather than drawing a rail across a heading", () => {
    const result = nestWorkers(
      groups({
        pinned: [row("lead", { pinned: true, workers: 1 })],
        open: [row("worker", { parentSessionId: "lead" }), row("other")],
      }),
      NONE,
    );

    expect(result.pinned.map((r) => r.sessionId)).toEqual(["lead", "worker"]);
    expect(result.pinned[1]?.depth).toBe(1);
    expect(result.open.map((r) => r.sessionId)).toEqual(["other"]);
  });

  it("leaves a worker in place at depth 0 when its lead is not on screen", () => {
    // The lead is archived, or a search filtered it out. Either way the worker
    // must stay visible and unindented rather than hang off nothing.
    const result = nestWorkers(
      groups({
        open: [row("orphan", { parentSessionId: "gone" })],
        archived: [row("gone", { archived: true })],
      }),
      NONE,
    );

    expect(result.open.map((r) => r.sessionId)).toEqual(["orphan"]);
    expect(result.open[0]?.depth).toBe(0);
    expect(result.open[0]?.collapsed).toBeUndefined();
  });

  it("never nests inside Archived", () => {
    const result = nestWorkers(
      groups({
        archived: [
          row("lead", { archived: true, workers: 1 }),
          row("worker", { archived: true, parentSessionId: "lead" }),
        ],
      }),
      NONE,
    );

    expect(result.archived.map((r) => r.depth)).toEqual([0, 0]);
  });

  it("hides a collapsed lead's workers and says so on the lead", () => {
    const result = nestWorkers(
      groups({
        open: [
          row("lead", { workers: 1 }),
          row("worker", { parentSessionId: "lead" }),
          row("tail"),
        ],
      }),
      new Set(["lead"]),
    );

    expect(result.open.map((r) => r.sessionId)).toEqual(["lead", "tail"]);
    expect(result.open[0]?.collapsed).toBe(true);
  });

  it("returns the input untouched when nothing nests", () => {
    const input = groups({ open: [row("a"), row("b")] });
    expect(nestWorkers(input, NONE)).toBe(input);
  });
});
