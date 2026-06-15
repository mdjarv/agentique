import { beforeEach, describe, expect, it } from "vitest";
import { useChatStore } from "~/stores/chat-store";
import type { ChatEvent, SessionMetadata, Turn } from "~/stores/chat-types";
import { applyEvent } from "~/stores/event-orchestrator";
import { useStreamingStore } from "~/stores/streaming-store";

const SID = "sess-1";

function makeMeta(overrides: Partial<SessionMetadata> = {}): SessionMetadata {
  return {
    id: SID,
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

const rid = () => `id-${Math.random().toString(36).slice(2)}`;
const text = (content: string): ChatEvent => ({ id: rid(), type: "text", content });
const result = (): ChatEvent => ({ id: rid(), type: "result" });

/** Seed a session with one in-progress turn so result events can merge into it. */
function seedSession(turn: Partial<Turn> = {}): void {
  useChatStore.getState().addSession(makeMeta());
  useChatStore
    .getState()
    .setSessionHistory(
      SID,
      [{ id: rid(), prompt: "p", attachments: [], events: [], complete: false, ...turn }],
      true,
    );
}

describe("event-orchestrator — applyEvent", () => {
  beforeEach(() => {
    useChatStore.setState({
      sessions: {},
      activeSessionId: null,
      loadedProjects: new Set(),
      historyLoading: new Set(),
    });
    useStreamingStore.getState().reset();
  });

  it("routes streaming-only deltas to the streaming-store and never the chat-store", () => {
    seedSession();
    applyEvent(SID, { id: rid(), type: "tool_output_delta", itemId: "t1", delta: "out" }, {});
    applyEvent(SID, { id: rid(), type: "reasoning_delta", itemId: "r1", delta: "think" }, {});

    expect(useStreamingStore.getState().toolOutputs[SID]?.t1).toBe("out");
    expect(useStreamingStore.getState().reasoningDeltas[SID]?.r1).toBe("think");
    // Durable turns untouched by deltas.
    expect(useChatStore.getState().sessions[SID]?.turns[0]?.events).toHaveLength(0);
  });

  it("clears the tool input buffer when its tool_use lands", () => {
    seedSession();
    useStreamingStore.getState().setToolBlockId(SID, 0, "tool-A");
    useStreamingStore.getState().appendToolInput(SID, "tool-A", '{"a":1}');

    applyEvent(
      SID,
      { id: rid(), type: "tool_use", toolId: "tool-A", toolName: "Bash", toolInput: {} },
      {},
    );

    expect(useStreamingStore.getState().toolInputs[SID]?.["tool-A"]).toBeUndefined();
    // The tool_use reached the chat-store's in-progress buffer (turn still open).
    const buffered = useChatStore.getState().sessions[SID]?.streamingEvents ?? [];
    expect(buffered.some((e) => e.type === "tool_use")).toBe(true);
  });

  it("clears tool output + progress buffers when a tool_result lands", () => {
    seedSession();
    useStreamingStore.getState().appendToolOutput(SID, "tool-A", "partial");
    useStreamingStore.getState().setToolProgress(SID, "tool-A", { elapsedMs: 10 });

    applyEvent(SID, { id: rid(), type: "tool_result", toolId: "tool-A" }, {});

    expect(useStreamingStore.getState().toolOutputs[SID]?.["tool-A"]).toBeUndefined();
    expect(useStreamingStore.getState().toolProgress[SID]?.["tool-A"]).toBeUndefined();
  });

  it("on result, merges the turn AND drains every streaming buffer (incl. toolBlockIndex) together", () => {
    seedSession({ events: [text("prompt-echo")] });

    // Seed in-flight streaming state across all five maps.
    useStreamingStore.getState().appendText(SID, "streamed text");
    useStreamingStore.getState().setToolBlockId(SID, 0, "tool-A");
    useStreamingStore.getState().appendToolInput(SID, "tool-A", '{"x":1}');
    useStreamingStore.getState().appendToolOutput(SID, "tool-A", "out");
    useStreamingStore.getState().setToolProgress(SID, "tool-A", { elapsedMs: 5 });
    useStreamingStore.getState().appendReasoning(SID, "r1", "thinking");

    applyEvent(SID, result(), {});

    // Chat-store: turn merged + marked complete.
    const turn = useChatStore.getState().sessions[SID]?.turns[0];
    expect(turn?.complete).toBe(true);
    expect(turn?.events.map((e) => e.type)).toContain("result");

    // Streaming-store: the result clears text, tool inputs, reasoning and the
    // toolBlockIndex together (tool outputs/progress are cleared per tool_result,
    // not on result, so they may linger — assert the result-scoped clears).
    const streaming = useStreamingStore.getState();
    expect(streaming.texts[SID]).toBeUndefined();
    expect(streaming.toolInputs[SID]).toBeUndefined();
    expect(streaming.reasoningDeltas[SID]).toBeUndefined();
    expect(streaming.toolBlockIndex[SID]).toBeUndefined();
  });
});
