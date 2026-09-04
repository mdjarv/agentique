import { beforeEach, describe, expect, it, vi } from "vitest";

vi.mock("~/lib/session/history", () => ({
  loadSessionHistory: vi.fn(),
  // No load in flight in these tests — nothing parks. The park/replay round
  // trip is exercised with the real module in history.test.ts.
  parkLiveEventDuringLoad: vi.fn(() => false),
  setLiveEventReplayer: vi.fn(),
}));

import { loadSessionHistory } from "~/lib/session/history";
import { ingestSessionEvent } from "~/lib/session/ingest";
import type { WsClient } from "~/lib/ws-client";
import { useChatStore } from "~/stores/chat-store";
import type { SessionMetadata } from "~/stores/chat-types";
import { useEventSeqStore } from "~/stores/event-seq";

/**
 * The wire-sequence gate must judge the RAW payload, not the parsed event.
 * Every stamped event advances the server's counter — including types this
 * build ignores (`workflow_launched`) or has never heard of (anything a newer
 * server adds). Gating after the parse made each of those a manufactured gap:
 * the next real event arrived at lastSeq+2 and forced a full-history resync
 * mid-turn.
 */

const ws = {} as WsClient;

function makeMeta(id: string): SessionMetadata {
  return {
    id,
    projectId: "proj-1",
    name: "Test Session",
    state: "running",
    connected: true,
    pinned: false,
    pinOrder: 0,
    model: "sonnet",
    permissionMode: "default",
    autoApproveMode: "manual",
    behaviorPresets: { autoCommit: true, suggestParallel: true, planFirst: false, terse: false },
    totalCost: 0,
    turnCount: 0,
    commitsAhead: 0,
    commitsBehind: 0,
    gitVersion: 0,
    createdAt: "2024-01-01T00:00:00Z",
    updatedAt: "2024-01-01T00:00:00Z",
  } as SessionMetadata;
}

function openSession(sid: string) {
  useChatStore.getState().addSession(makeMeta(sid));
  useChatStore.getState().submitQuery(sid, "do the thing");
}

function push(sid: string, seq: number, event: Record<string, unknown>, epoch = 7) {
  ingestSessionEvent(ws, { sessionId: sid, event, seq, epoch });
}

const textEvent = (content: string) => ({ type: "text", content });

function appliedTexts(sid: string): string[] {
  const session = useChatStore.getState().sessions[sid];
  if (!session) return [];
  return session.streamingEvents
    .filter((e) => e.type === "text")
    .map((e) => ("content" in e ? (e.content as string) : ""));
}

describe("ingestSessionEvent", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    useEventSeqStore.getState().reset();
    useChatStore.setState({
      sessions: {},
      activeSessionId: null,
      loadedProjects: new Set(),
      historyLoading: new Set(),
    });
  });

  it("applies in-order events and tracks the wire seq", () => {
    openSession("s1");
    push("s1", 1, textEvent("a"));
    push("s1", 2, textEvent("b"));

    expect(appliedTexts("s1")).toEqual(["a", "b"]);
    expect(useEventSeqStore.getState().states.s1).toEqual({ epoch: 7, lastSeq: 2 });
    expect(loadSessionHistory).not.toHaveBeenCalled();
  });

  // The regression: workflow_launched IS persisted and seq-stamped on the
  // server, but parseServerEvent ignores it. Its seq must count anyway, or the
  // next real event reads as a gap and forces a resync mid-turn.
  it("advances the seq for an ignored event type instead of manufacturing a gap", () => {
    openSession("s1");
    push("s1", 1, textEvent("a"));
    push("s1", 2, { type: "workflow_launched", runId: "wf_x" });
    push("s1", 3, textEvent("b"));

    expect(appliedTexts("s1")).toEqual(["a", "b"]);
    expect(useEventSeqStore.getState().states.s1?.lastSeq).toBe(3);
    expect(loadSessionHistory).not.toHaveBeenCalled();
  });

  // Same argument one release forward: a newer server ships an event type this
  // build has never heard of. It is stamped like everything else.
  it("advances the seq for an unknown event type from a newer server", () => {
    const warn = vi.spyOn(console, "warn").mockImplementation(() => {});
    try {
      openSession("s1");
      push("s1", 1, textEvent("a"));
      push("s1", 2, { type: "event_from_the_future" });
      push("s1", 3, textEvent("b"));

      expect(appliedTexts("s1")).toEqual(["a", "b"]);
      expect(useEventSeqStore.getState().states.s1?.lastSeq).toBe(3);
      expect(loadSessionHistory).not.toHaveBeenCalled();
    } finally {
      warn.mockRestore();
    }
  });

  it("still resyncs on a real gap", () => {
    openSession("s1");
    push("s1", 1, textEvent("a"));
    push("s1", 3, textEvent("c"));

    expect(loadSessionHistory).toHaveBeenCalledTimes(1);
    expect(loadSessionHistory).toHaveBeenCalledWith(ws, "s1", true);
  });

  it("drops a duplicate before it reaches the stores", () => {
    openSession("s1");
    push("s1", 1, textEvent("a"));
    push("s1", 1, textEvent("a"));

    expect(appliedTexts("s1")).toEqual(["a"]);
    expect(loadSessionHistory).not.toHaveBeenCalled();
  });

  // seq 0 = unsequenced (e.g. a channel message to an offline session):
  // applied directly, and the gate's state is left alone.
  it("applies an unsequenced event without touching the seq state", () => {
    openSession("s1");
    push("s1", 1, textEvent("a"));
    push("s1", 0, textEvent("aside"));

    expect(appliedTexts("s1")).toEqual(["a", "aside"]);
    expect(useEventSeqStore.getState().states.s1?.lastSeq).toBe(1);
  });

  // Bug 4's contract: a paired machine on an older release sends no seq/epoch
  // at all. Absent reads as unsequenced, never as a schema violation.
  it("treats an absent seq as unsequenced", () => {
    openSession("s1");
    ingestSessionEvent(ws, { sessionId: "s1", event: textEvent("old peer") });

    expect(appliedTexts("s1")).toEqual(["old peer"]);
    expect(useEventSeqStore.getState().states.s1).toBeUndefined();
  });
});
