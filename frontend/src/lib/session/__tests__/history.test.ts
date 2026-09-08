import { beforeEach, describe, expect, it, vi } from "vitest";
import type { HistoryResult } from "~/lib/generated-types";
import { loadSessionHistory } from "~/lib/session/history";
// Importing ingest registers the live-event replayer with history.ts — the
// same wiring the app gets. These tests exercise the REAL park/replay round
// trip; ingest.test.ts covers the gate in isolation with history mocked.
import { ingestSessionEvent } from "~/lib/session/ingest";
import type { WsClient } from "~/lib/ws-client";
import { useChatStore } from "~/stores/chat-store";
import type { ChatEvent, SessionMetadata } from "~/stores/chat-types";
import { useEventSeqStore } from "~/stores/event-seq";

/**
 * A history snapshot replaces a session's turns wholesale and reseeds the
 * wire-seq tracker to its high-water mark. Live events accepted during the
 * fetch's round trip were wiped by that replacement and never redelivered —
 * the rewound tracker read them as already-seen. If the wiped window held the
 * turn's result, the turn rendered permanently unfinished. The fix parks live
 * payloads while a load is in flight and replays them through the gate after
 * the snapshot lands.
 */

const SID = "sess-hist";

function makeMeta(id = SID): SessionMetadata {
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

function deferred<T>() {
  let resolve!: (v: T) => void;
  let reject!: (e: unknown) => void;
  const promise = new Promise<T>((res, rej) => {
    resolve = res;
    reject = rej;
  });
  return { promise, resolve, reject };
}

function snapshot(
  turnEvents: Record<string, unknown>[][],
  opts: { epoch?: number; highWaterSeq: number },
): HistoryResult {
  return {
    turns: turnEvents.map((events, i) => ({
      prompt: `prompt-${i}`,
      events,
      turnIndex: i + 1,
    })),
    hasMore: false,
    totalTurns: turnEvents.length,
    epoch: opts.epoch ?? 7,
    highWaterSeq: opts.highWaterSeq,
  };
}

/** A session whose history is loaded and whose seq tracker sits at (7, 5). */
function seedLoadedSession() {
  useChatStore.getState().addSession(makeMeta());
  useChatStore.getState().setSessionHistory(
    SID,
    [
      {
        id: "turn-1",
        prompt: "p",
        attachments: [],
        events: [{ id: "e1", type: "text", content: "old" } as ChatEvent],
        complete: true,
      },
    ],
    true,
  );
  useEventSeqStore.getState().seedFromHistory(SID, 7, 5);
}

function lastTurnContents(): string[] {
  const session = useChatStore.getState().sessions[SID];
  const turn = session?.turns[session.turns.length - 1];
  return (turn?.events ?? [])
    .filter((e) => e.type === "text")
    .map((e) => ("content" in e ? (e.content as string) : ""));
}

const liveText = (content: string, seq: number) =>
  ({ sessionId: SID, event: { type: "text", content }, seq, epoch: 7 }) as const;

async function loadingSettled(id = SID) {
  await vi.waitFor(() => {
    expect(useChatStore.getState().historyLoading.has(id)).toBe(false);
  });
}

describe("loadSessionHistory — live events during the fetch window", () => {
  beforeEach(() => {
    useEventSeqStore.getState().reset();
    useChatStore.setState({
      sessions: {},
      activeSessionId: null,
      loadedProjects: new Set(),
      historyLoading: new Set(),
    });
    seedLoadedSession();
  });

  it("parks a live event during a force load and replays it after the snapshot", async () => {
    const d = deferred<HistoryResult>();
    const ws = { request: vi.fn(() => d.promise) } as unknown as WsClient;

    loadSessionHistory(ws, SID, { force: true });
    expect(useChatStore.getState().historyLoading.has(SID)).toBe(true);

    // Lands mid-round-trip, above the snapshot's high-water mark — the exact
    // event the old code wiped.
    ingestSessionEvent(ws, liveText("mid-load", 6));

    // Parked: nothing applied, tracker untouched.
    expect(lastTurnContents()).toEqual(["old"]);
    expect(useEventSeqStore.getState().states[SID]?.lastSeq).toBe(5);

    d.resolve(
      snapshot([[{ type: "text", content: "from-snapshot" }, { type: "result" }]], {
        highWaterSeq: 5,
      }),
    );
    await loadingSettled();

    // The snapshot landed AND the parked event survived it.
    expect(lastTurnContents()).toEqual(["from-snapshot", "mid-load"]);
    expect(useEventSeqStore.getState().states[SID]?.lastSeq).toBe(6);
  });

  it("drops a parked event the snapshot already contains", async () => {
    const d = deferred<HistoryResult>();
    const ws = { request: vi.fn(() => d.promise) } as unknown as WsClient;

    loadSessionHistory(ws, SID, { force: true });
    ingestSessionEvent(ws, liveText("mid-load", 6));

    // The server processed seq 6 before answering, so the snapshot includes
    // it and its high-water mark covers it.
    d.resolve(
      snapshot([[{ type: "text", content: "mid-load" }, { type: "result" }]], {
        highWaterSeq: 6,
      }),
    );
    await loadingSettled();

    // Replayed through the gate, judged a duplicate, applied exactly once.
    expect(lastTurnContents()).toEqual(["mid-load"]);
    expect(useEventSeqStore.getState().states[SID]?.lastSeq).toBe(6);
  });

  it("parks the gap event that triggers a resync instead of applying it", async () => {
    const d = deferred<HistoryResult>();
    const ws = { request: vi.fn(() => d.promise) } as unknown as WsClient;

    // seq 8 against lastSeq 5: a gap, so ingest starts the force load itself.
    ingestSessionEvent(ws, liveText("trigger", 8));

    expect(ws.request).toHaveBeenCalledTimes(1);
    expect(useChatStore.getState().historyLoading.has(SID)).toBe(true);
    // The trigger neither applied nor advanced the tracker — the snapshot it
    // caused will contain it.
    expect(lastTurnContents()).toEqual(["old"]);
    expect(useEventSeqStore.getState().states[SID]?.lastSeq).toBe(5);

    d.resolve(
      snapshot(
        [
          [
            { type: "text", content: "missed" },
            { type: "text", content: "trigger" },
            { type: "result" },
          ],
        ],
        { highWaterSeq: 8 },
      ),
    );
    await loadingSettled();

    expect(lastTurnContents()).toEqual(["missed", "trigger"]);
    expect(useEventSeqStore.getState().states[SID]?.lastSeq).toBe(8);
    // The replayed trigger was a duplicate of the snapshot — no second load.
    expect(ws.request).toHaveBeenCalledTimes(1);
  });

  it("seeds the tracker from an empty snapshot so replays are not re-read as gaps", async () => {
    const d = deferred<HistoryResult>();
    const ws = { request: vi.fn(() => d.promise) } as unknown as WsClient;

    loadSessionHistory(ws, SID, { force: true });
    ingestSessionEvent(ws, liveText("after-empty", 9));

    // Empty turns, but the response still names the authoritative high-water
    // mark. Without the seed, the replay of seq 9 read its own arrival as a
    // gap against the stale (7, 5) state and started another load.
    d.resolve(snapshot([], { highWaterSeq: 8 }));
    await loadingSettled();

    expect(useEventSeqStore.getState().states[SID]?.lastSeq).toBe(9);
    expect(lastTurnContents()).toEqual(["old", "after-empty"]);
    expect(ws.request).toHaveBeenCalledTimes(1);
  });

  it("drains parked events on a failed load instead of swallowing them", async () => {
    const err = vi.spyOn(console, "error").mockImplementation(() => {});
    try {
      const d = deferred<HistoryResult>();
      const requests: Array<ReturnType<typeof deferred<HistoryResult>>> = [];
      const ws = {
        request: vi.fn(() => {
          const next = deferred<HistoryResult>();
          requests.push(next);
          return requests.length === 1 ? d.promise : next.promise;
        }),
      } as unknown as WsClient;

      loadSessionHistory(ws, SID, { force: true });
      ingestSessionEvent(ws, liveText("survivor", 8));

      d.reject(new Error("socket closed"));
      // The drain replays against the pre-load state: seq 8 is a genuine gap
      // there, so the survivor parks into a retry load rather than vanishing.
      await vi.waitFor(() => {
        expect(ws.request).toHaveBeenCalledTimes(2);
      });
      expect(useChatStore.getState().historyLoading.has(SID)).toBe(true);
    } finally {
      err.mockRestore();
    }
  });
});

/**
 * The session on screen holds its full history; every other session holds a
 * tail. These pin the shape each call leaves behind, and the number of round
 * trips it costs — the boot flood was every open session fetching everything.
 */
// Its own session id: the parking tests above leave a retry load parked on
// SID on purpose, and a parked event replayed against these immediate mocks
// would resync forever.
const HELD = "sess-held";

function heldTurnContents(): string[] {
  const session = useChatStore.getState().sessions[HELD];
  const turn = session?.turns[session.turns.length - 1];
  return (turn?.events ?? [])
    .filter((e) => e.type === "text")
    .map((e) => ("content" in e ? (e.content as string) : ""));
}

describe("loadSessionHistory — what a session holds", () => {
  beforeEach(() => {
    useEventSeqStore.getState().reset();
    useChatStore.setState({
      sessions: {},
      activeSessionId: null,
      loadedProjects: new Set(),
      historyLoading: new Set(),
    });
    useChatStore.getState().addSession(makeMeta(HELD));
  });

  function tailThenFull(hasMore: boolean) {
    const calls: Array<{ limit?: number }> = [];
    const ws = {
      request: vi.fn((_type: string, payload: { limit?: number }) => {
        calls.push(payload);
        const turns = payload.limit
          ? [[{ type: "text", content: "newest" }, { type: "result" }]]
          : [
              [{ type: "text", content: "oldest" }, { type: "result" }],
              [{ type: "text", content: "newest" }, { type: "result" }],
            ];
        return Promise.resolve({
          ...snapshot(turns, { highWaterSeq: 5 }),
          hasMore: payload.limit ? hasMore : false,
          totalTurns: 2,
        });
      }),
    } as unknown as WsClient;
    return { ws, calls };
  }

  it("a tail load costs one round trip and leaves the session incomplete but not loading", async () => {
    const { ws, calls } = tailThenFull(true);
    loadSessionHistory(ws, HELD, { tail: true });
    await loadingSettled(HELD);

    expect(calls).toEqual([{ sessionId: HELD, limit: 20 }]);
    const session = useChatStore.getState().sessions[HELD];
    expect(session?.turns.map((t) => t.events.length)).toEqual([2]);
    expect(session?.historyComplete).toBe(false);
    // Authoritative about sequencing even though it is only the tail.
    expect(useEventSeqStore.getState().states[HELD]?.lastSeq).toBe(5);
  });

  it("a tail is not refetched while one is held, unless forced", async () => {
    const { ws, calls } = tailThenFull(true);
    loadSessionHistory(ws, HELD, { tail: true });
    await loadingSettled(HELD);
    loadSessionHistory(ws, HELD, { tail: true });
    expect(calls).toHaveLength(1);

    loadSessionHistory(ws, HELD, { tail: true, force: true });
    await loadingSettled(HELD);
    expect(calls).toHaveLength(2);
    expect(useChatStore.getState().sessions[HELD]?.historyComplete).toBe(false);
  });

  it("a full load on a held tail goes straight to the full snapshot", async () => {
    const { ws, calls } = tailThenFull(true);
    loadSessionHistory(ws, HELD, { tail: true });
    await loadingSettled(HELD);

    loadSessionHistory(ws, HELD);
    await loadingSettled(HELD);
    await vi.waitFor(() => {
      expect(useChatStore.getState().sessions[HELD]?.historyComplete).toBe(true);
    });
    expect(calls).toEqual([{ sessionId: HELD, limit: 20 }, { sessionId: HELD }]);
    expect(heldTurnContents()).toEqual(["newest"]);
    expect(useChatStore.getState().sessions[HELD]?.turns).toHaveLength(2);
  });

  it("a cold full load is the tail, then the rest — or just the tail when that is everything", async () => {
    const more = tailThenFull(true);
    loadSessionHistory(more.ws, HELD);
    await vi.waitFor(() => {
      expect(useChatStore.getState().sessions[HELD]?.historyComplete).toBe(true);
    });
    expect(more.calls).toEqual([{ sessionId: HELD, limit: 20 }, { sessionId: HELD }]);

    useChatStore.setState({ sessions: {}, historyLoading: new Set() });
    useChatStore.getState().addSession(makeMeta(HELD));
    const all = tailThenFull(false);
    loadSessionHistory(all.ws, HELD);
    await loadingSettled(HELD);
    expect(all.calls).toEqual([{ sessionId: HELD, limit: 20 }]);
    expect(useChatStore.getState().sessions[HELD]?.historyComplete).toBe(true);
  });
});
