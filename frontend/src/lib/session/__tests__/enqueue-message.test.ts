import { beforeEach, describe, expect, it, vi } from "vitest";
import { enqueueMessage } from "~/lib/session/actions";
import type { WsClient } from "~/lib/ws-client";
import { useChatStore } from "~/stores/chat-store";
import type { SessionMetadata } from "~/stores/chat-types";

// The composer decides optimistic-vs-not from the last pushed session state,
// which can be a round trip behind the server. session.enqueue answers with what
// the server actually did, and enqueueMessage settles the guess against it —
// otherwise a message delivered into a running turn shows up twice: once as the
// turn the client drew, once as the echo the server broadcast.

function makeMeta(overrides: Partial<SessionMetadata> = {}): SessionMetadata {
  return {
    id: "sess-1",
    projectId: "proj-1",
    name: "Test Session",
    state: "idle",
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
    ...overrides,
  };
}

function wsAnswering(result: unknown): WsClient {
  return { request: vi.fn().mockResolvedValue(result) } as unknown as WsClient;
}

const turnPrompts = () =>
  useChatStore.getState().sessions["sess-1"]?.turns.map((t) => t.prompt) ?? [];

describe("enqueueMessage — reconciling the optimistic turn", () => {
  beforeEach(() => {
    useChatStore.setState({ sessions: {}, activeSessionId: null });
    // State says idle, so the composer draws the turn optimistically.
    useChatStore.getState().addSession(makeMeta({ state: "idle" }));
  });

  it("keeps the optimistic turn when the server opened a turn for it", async () => {
    await enqueueMessage(wsAnswering({ delivery: "turn" }), "sess-1", "hello");
    expect(turnPrompts()).toEqual(["hello"]);
  });

  it("drops it when the server injected the message into a running turn", async () => {
    await enqueueMessage(wsAnswering({ delivery: "mid_turn" }), "sess-1", "hello");
    expect(turnPrompts()).toEqual([]);
  });

  it("drops it when the server queued the message for the next turn", async () => {
    await enqueueMessage(wsAnswering({ delivery: "queued" }), "sess-1", "hello");
    expect(turnPrompts()).toEqual([]);
  });

  // A peer on an older release answers with an empty object. Absent is unknown,
  // not "turn" — leave the guess standing, which is the pre-existing behaviour.
  it("leaves the guess alone when the peer does not report a delivery", async () => {
    await enqueueMessage(wsAnswering({}), "sess-1", "hello");
    expect(turnPrompts()).toEqual(["hello"]);
  });

  it("rolls back and rethrows when the send fails", async () => {
    const ws = { request: vi.fn().mockRejectedValue(new Error("nope")) } as unknown as WsClient;
    await expect(enqueueMessage(ws, "sess-1", "hello")).rejects.toThrow("nope");
    expect(turnPrompts()).toEqual([]);
  });

  it("draws no turn at all when the session is already known to be running", async () => {
    useChatStore.setState({ sessions: {}, activeSessionId: null });
    useChatStore.getState().addSession(makeMeta({ state: "running" }));
    await enqueueMessage(wsAnswering({ delivery: "mid_turn" }), "sess-1", "hello");
    expect(turnPrompts()).toEqual([]);
  });
});
