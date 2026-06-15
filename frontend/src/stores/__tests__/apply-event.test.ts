import { describe, expect, it } from "vitest";
import { applyServerEvent } from "~/stores/apply-event";
import type {
  ChatEvent,
  SessionData,
  SessionMetadata,
  Turn,
  UserMessageEvent,
} from "~/stores/chat-types";

// --- Builders -------------------------------------------------------------

const rid = () => `id-${Math.random().toString(36).slice(2)}`;

function makeMeta(overrides: Partial<SessionMetadata> = {}): SessionMetadata {
  return {
    id: "sess-1",
    projectId: "proj-1",
    name: "Test Session",
    state: "running",
    connected: true,
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

function makeSession(overrides: Partial<SessionData> = {}): SessionData {
  return {
    meta: makeMeta(),
    turns: [],
    streamingEvents: [],
    historyComplete: true,
    hasUnseenCompletion: false,
    hasUnreadChannelMessage: false,
    pendingApproval: null,
    pendingQuestion: null,
    planMode: false,
    autoApproveMode: "manual",
    todos: null,
    contextUsage: null,
    compacting: false,
    ...overrides,
  };
}

function makeTurn(overrides: Partial<Turn> = {}): Turn {
  return { id: rid(), prompt: "", attachments: [], events: [], complete: false, ...overrides };
}

const text = (content: string): ChatEvent => ({ id: rid(), type: "text", content });
const result = (overrides: Partial<Extract<ChatEvent, { type: "result" }>> = {}): ChatEvent => ({
  id: rid(),
  type: "result",
  ...overrides,
});
const userMsg = (overrides: Partial<UserMessageEvent> = {}): UserMessageEvent => ({
  id: rid(),
  type: "user_message",
  ...overrides,
});
const taskProgress = (toolUseId: string, taskSummary: string): ChatEvent => ({
  id: rid(),
  type: "task",
  taskSubtype: "task_progress",
  toolUseId,
  taskSummary,
});

// --- Transient / no-op events --------------------------------------------

describe("applyServerEvent — transient events", () => {
  it.each([
    "stream",
    "context_management",
    "tool_output_delta",
    "reasoning_delta",
    "tool_progress",
  ])("returns null for %s (no patch, no buffer growth)", (type) => {
    const session = makeSession({ turns: [makeTurn()] });
    // itemId/delta/etc. are required on some of these but applyServerEvent
    // discriminates on `type` before reading them.
    const res = applyServerEvent(session, { id: rid(), type } as unknown as ChatEvent, true);
    expect(res).toBeNull();
  });

  it("rate_limit returns an empty patch plus a side-effect descriptor (stays pure)", () => {
    const session = makeSession();
    const res = applyServerEvent(
      session,
      {
        id: rid(),
        type: "rate_limit",
        rateLimitType: "seven_day",
        status: "throttled",
        utilization: 0.5,
        resetsAt: 123,
      },
      true,
    );
    expect(res).not.toBeNull();
    expect(res?.patch).toEqual({});
    expect(res?.sideEffect).toEqual({
      type: "rate_limit",
      rateLimitType: "seven_day",
      status: "throttled",
      utilization: 0.5,
      resetsAt: 123,
    });
  });

  it("rate_limit defaults an unknown window to five_hour", () => {
    const res = applyServerEvent(makeSession(), { id: rid(), type: "rate_limit" }, true);
    expect(res?.sideEffect?.rateLimitType).toBe("five_hour");
  });

  it("compact_status toggles compacting; compact_boundary clears it", () => {
    const session = makeSession({ turns: [makeTurn()] });
    expect(
      applyServerEvent(session, { id: rid(), type: "compact_status", status: "compacting" }, true)
        ?.patch.compacting,
    ).toBe(true);
    expect(
      applyServerEvent(session, { id: rid(), type: "compact_status", status: "done" }, true)?.patch
        .compacting,
    ).toBe(false);
    expect(
      applyServerEvent(session, { id: rid(), type: "compact_boundary" }, true)?.patch.compacting,
    ).toBe(false);
  });
});

// --- Streaming append ------------------------------------------------------

describe("applyServerEvent — streaming buffer", () => {
  it("appends a normal event to streamingEvents and leaves turns referentially stable", () => {
    const turn = makeTurn({ events: [text("hi")] });
    const session = makeSession({ turns: [turn], streamingEvents: [text("a")] });
    const res = applyServerEvent(session, text("b"), true);
    expect(res?.patch.turns).toBeUndefined(); // turns untouched while streaming
    expect(res?.patch.streamingEvents).toHaveLength(2);
    expect((res?.patch.streamingEvents?.[1] as { content: string }).content).toBe("b");
  });

  it("stamps a user_message with deliveryStatus=sending (or queued when queued)", () => {
    const session = makeSession({ turns: [makeTurn()] });
    const sending = applyServerEvent(session, userMsg({ messageId: "m1", content: "x" }), true);
    expect((sending?.patch.streamingEvents?.[0] as UserMessageEvent).deliveryStatus).toBe(
      "sending",
    );

    const queued = applyServerEvent(
      session,
      userMsg({ messageId: "m2", content: "x", queued: true }),
      true,
    );
    expect((queued?.patch.streamingEvents?.[0] as UserMessageEvent).deliveryStatus).toBe("queued");
  });

  it("upserts task_progress in place by toolUseId instead of duplicating", () => {
    const session = makeSession({
      turns: [makeTurn()],
      streamingEvents: [taskProgress("t1", "first")],
    });
    const res = applyServerEvent(session, taskProgress("t1", "second"), true);
    expect(res?.patch.streamingEvents).toHaveLength(1);
    expect((res?.patch.streamingEvents?.[0] as { taskSummary: string }).taskSummary).toBe("second");
  });

  it("appends task_progress for a new toolUseId", () => {
    const session = makeSession({
      turns: [makeTurn()],
      streamingEvents: [taskProgress("t1", "first")],
    });
    const res = applyServerEvent(session, taskProgress("t2", "other"), true);
    expect(res?.patch.streamingEvents).toHaveLength(2);
  });
});

// --- Result boundary -------------------------------------------------------

describe("applyServerEvent — result merge", () => {
  it("merges the streaming buffer into the last turn and marks it complete", () => {
    const turn = makeTurn({ events: [text("prompt-echo")] });
    const session = makeSession({ turns: [turn], streamingEvents: [text("streamed")] });
    const res = applyServerEvent(session, result(), false);

    const merged = res?.patch.turns?.[0];
    expect(merged?.complete).toBe(true);
    expect(merged?.events.map((e) => e.type)).toEqual(["text", "text", "result"]);
    expect(res?.patch.streamingEvents).toEqual([]); // buffer drained
    expect(res?.patch.meta?.state).toBe("idle");
  });

  it("keeps queued messages in the buffer across the result boundary (carry-over)", () => {
    const turn = makeTurn({ events: [] });
    const queued = userMsg({ messageId: "q1", content: "next turn", deliveryStatus: "queued" });
    const session = makeSession({ turns: [turn], streamingEvents: [text("streamed"), queued] });
    const res = applyServerEvent(session, result(), true);

    // The non-queued streamed event merges into the turn...
    expect(res?.patch.turns?.[0]?.events.map((e) => e.type)).toEqual(["text", "result"]);
    // ...but the queued message survives for the next (replayed) turn.
    expect(res?.patch.streamingEvents).toHaveLength(1);
    expect((res?.patch.streamingEvents?.[0] as UserMessageEvent).messageId).toBe("q1");
  });

  it("sets hasUnseenCompletion only when the session is not being viewed", () => {
    const session = makeSession({ turns: [makeTurn()] });
    expect(applyServerEvent(session, result(), false)?.patch.hasUnseenCompletion).toBe(true);
    expect(applyServerEvent(session, result(), true)?.patch.hasUnseenCompletion).toBe(false);
  });

  it("derives contextUsage from a result carrying a context window", () => {
    const session = makeSession({ turns: [makeTurn()] });
    const res = applyServerEvent(
      session,
      result({ contextWindow: 200_000, inputTokens: 1000, outputTokens: 500 }),
      true,
    );
    expect(res?.patch.contextUsage).toEqual({
      contextWindow: 200_000,
      inputTokens: 1000,
      outputTokens: 500,
    });
  });

  it("falls back to prior contextUsage token counts when the result omits them", () => {
    const session = makeSession({
      turns: [makeTurn()],
      contextUsage: { contextWindow: 200_000, inputTokens: 9, outputTokens: 8 },
    });
    const res = applyServerEvent(session, result({ contextWindow: 200_000 }), true);
    expect(res?.patch.contextUsage).toEqual({
      contextWindow: 200_000,
      inputTokens: 9,
      outputTokens: 8,
    });
  });
});

// --- Late events & delivery acks ------------------------------------------

describe("applyServerEvent — late append to a complete turn", () => {
  it("appends to the last turn (not the buffer) when it is already complete", () => {
    const turn = makeTurn({ events: [text("done")], complete: true });
    const session = makeSession({ turns: [turn], streamingEvents: [] });
    const res = applyServerEvent(session, text("late"), true);
    expect(res?.patch.streamingEvents).toBeUndefined();
    expect(res?.patch.turns?.[0]?.events.map((e) => (e as { content: string }).content)).toEqual([
      "done",
      "late",
    ]);
    // The turn stays complete (the late append doesn't reopen it).
    expect(res?.patch.turns?.[0]?.complete).toBe(true);
  });
});

describe("applyServerEvent — message_delivery acks", () => {
  it("marks a buffered message delivered (preferred location)", () => {
    const msg = userMsg({ messageId: "m1", content: "x", deliveryStatus: "sending" });
    const session = makeSession({ turns: [makeTurn()], streamingEvents: [msg] });
    const res = applyServerEvent(
      session,
      { id: rid(), type: "message_delivery", messageId: "m1" },
      true,
    );
    expect((res?.patch.streamingEvents?.[0] as UserMessageEvent).deliveryStatus).toBe("delivered");
    expect(res?.patch.turns).toBeUndefined();
  });

  it("falls back to a committed turn event when the buffer has no match", () => {
    const msg = userMsg({ messageId: "m2", content: "x", deliveryStatus: "sending" });
    const turn = makeTurn({ events: [msg], complete: true });
    const session = makeSession({ turns: [turn], streamingEvents: [] });
    const res = applyServerEvent(
      session,
      { id: rid(), type: "message_delivery", messageId: "m2" },
      true,
    );
    expect((res?.patch.turns?.[0]?.events[0] as UserMessageEvent).deliveryStatus).toBe("delivered");
    expect(res?.patch.streamingEvents).toBeUndefined();
  });

  it("ignores a message_delivery without a messageId", () => {
    const session = makeSession({ turns: [makeTurn()] });
    const res = applyServerEvent(session, { id: rid(), type: "message_delivery" }, true);
    // No messageId → falls through to the generic buffer append path, not the ack path.
    expect(res?.patch.streamingEvents).toBeDefined();
  });
});
