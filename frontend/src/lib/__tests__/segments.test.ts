import { describe, expect, it } from "vitest";
import {
  type ActivityItem,
  buildSegments,
  buildTurnSections,
  classifyEvent,
  type Segment,
} from "~/lib/segments";
import type { ChatEvent } from "~/stores/chat-types";

const rid = () => `id-${Math.random().toString(36).slice(2)}`;
const text = (content: string, timestamp?: number): ChatEvent => ({
  id: rid(),
  type: "text",
  content,
  timestamp,
});
const errorEvent = (content: string): ChatEvent => ({ id: rid(), type: "error", content });
const userMessage = (
  content: string,
  deliveryStatus?: "sending" | "delivered" | "queued",
): ChatEvent => ({ id: rid(), type: "user_message", content, deliveryStatus });
const agentMessage = (
  overrides: Partial<Extract<ChatEvent, { type: "agent_message" }>>,
): ChatEvent => ({
  id: rid(),
  type: "agent_message",
  ...overrides,
});
const CHANNEL_SEND_TOOL = "mcp__agentique-channel__SendMessage";

function toolUse(toolId: string, toolName: string, toolInput: unknown, id = toolId): ChatEvent {
  return { id, type: "tool_use", toolId, toolName, toolInput };
}

function toolResult(toolId: string, text: string): ChatEvent {
  return {
    id: `${toolId}-r`,
    type: "tool_result",
    toolId,
    contentBlocks: [{ type: "text", text }],
  };
}

const isTool = (i: ActivityItem): i is Extract<ActivityItem, { kind: "tool" }> => i.kind === "tool";

function toolItems(segments: Segment[]) {
  return segments
    .filter((s): s is Extract<Segment, { kind: "activity" }> => s.kind === "activity")
    .flatMap((s) => s.items)
    .filter(isTool);
}

describe("buildSegments tool dedupe", () => {
  it("merges a pending-approval tool_use and the started tool_use sharing an item ID", () => {
    // Codex emits the approval-derived tool_use and the started item with the
    // same ID; they must collapse to one element with the result attached.
    const events: ChatEvent[] = [
      toolUse("call_1", "Bash", {}), // pending approval — sparse input
      toolUse("call_1", "Bash", { command: "git status" }), // started — full input
      toolResult("call_1", "clean"),
    ];

    const items = toolItems(buildSegments(events, true).segments);

    expect(items).toHaveLength(1);
    expect(items[0]?.use.toolInput).toEqual({ command: "git status" }); // later event wins
    expect(items[0]?.result?.contentBlocks?.[0]?.text).toBe("clean");
  });

  it("keeps fanned-out fileChange files (#N) as separate, individually correlated items", () => {
    const events: ChatEvent[] = [
      toolUse("item_9#0", "Edit", { file_path: "a.ts", diff: "@@ a" }),
      toolUse("item_9#1", "Write", { file_path: "b.ts", diff: "@@ b" }),
      toolResult("item_9#0", "diff-a"),
      toolResult("item_9#1", "diff-b"),
    ];

    const items = toolItems(buildSegments(events, true).segments);

    expect(items).toHaveLength(2);
    expect(items[0]?.result?.contentBlocks?.[0]?.text).toBe("diff-a");
    expect(items[1]?.result?.contentBlocks?.[0]?.text).toBe("diff-b");
  });
});

describe("classifyEvent", () => {
  it.each([
    ["thinking", "activity"],
    ["tool_use", "activity"],
    ["tool_result", "activity"],
    ["text", "text"],
    ["error", "error"],
    ["result", "result"],
    ["compact_boundary", "compact"],
    ["user_message", "user_message"],
    ["agent_message", "agent_message"],
    ["task", "skip"],
    ["tool_progress", "skip"],
    ["reasoning_delta", "skip"],
    ["turn_diff", "skip"],
  ])("classifies %s as %s", (type, expected) => {
    expect(classifyEvent({ id: rid(), type } as unknown as ChatEvent)).toBe(expected);
  });
});

describe("buildSegments text + error grouping", () => {
  it("merges consecutive text events with a blank line and tracks the latest timestamp", () => {
    const { segments } = buildSegments([text("one", 100), text("two", 200)], true);
    expect(segments).toHaveLength(1);
    const seg = segments[0] as Extract<Segment, { kind: "text" }>;
    expect(seg.content).toBe("one\n\ntwo");
    expect(seg.timestamp).toBe(200);
  });

  it("groups consecutive errors into one error segment", () => {
    const { segments } = buildSegments([errorEvent("boom"), errorEvent("again")], true);
    const errs = segments.filter((s) => s.kind === "error");
    expect(errs).toHaveLength(1);
    expect((errs[0] as Extract<Segment, { kind: "error" }>).events).toHaveLength(2);
  });

  it("extracts the result event without emitting a segment for it", () => {
    const { segments, resultEvent } = buildSegments(
      [text("hi"), { id: rid(), type: "result", contextWindow: 1 }],
      true,
    );
    expect(resultEvent?.contextWindow).toBe(1);
    // Only the text segment remains; the result is returned separately, not emitted.
    expect(segments).toHaveLength(1);
    expect(segments[0]?.kind).toBe("text");
  });
});

describe("buildSegments channel-send interception", () => {
  it("turns a channel SendMessage tool_use into a channel_send segment and suppresses its result", () => {
    const events: ChatEvent[] = [
      toolUse("call_x", CHANNEL_SEND_TOOL, { to: "Worker", message: "ping", type: "progress" }),
      toolResult("call_x", "delivered"),
    ];
    const { segments } = buildSegments(events, true);
    expect(segments).toHaveLength(1);
    const seg = segments[0] as Extract<Segment, { kind: "channel_send" }>;
    expect(seg).toMatchObject({
      kind: "channel_send",
      to: "Worker",
      message: "ping",
      messageType: "progress",
      toolId: "call_x",
    });
    // The tool_result must NOT have attached to a tool activity item.
    expect(toolItems(segments)).toHaveLength(0);
  });
});

const SUGGEST_SESSION_TOOL = "mcp__agentique__SuggestSessionPrompt";

describe("buildSegments suggest-session interception", () => {
  it("turns a SuggestSessionPrompt tool_use into a suggest_session segment and suppresses its result", () => {
    const events: ChatEvent[] = [
      toolUse("call_s", SUGGEST_SESSION_TOOL, {
        title: "Refactor auth",
        prompt: "Refactor the auth middleware.",
        project: "backend",
      }),
      toolResult("call_s", "Suggestion surfaced to the user as a launchable card."),
    ];
    const { segments } = buildSegments(events, true);
    expect(segments).toHaveLength(1);
    const seg = segments[0] as Extract<Segment, { kind: "suggest_session" }>;
    expect(seg).toMatchObject({
      kind: "suggest_session",
      title: "Refactor auth",
      prompt: "Refactor the auth middleware.",
      projectSlug: "backend",
      toolId: "call_s",
    });
    // The benign ack tool_result must NOT render as a tool activity item.
    expect(toolItems(segments)).toHaveLength(0);
  });

  it("omits projectSlug when no project is given (current-project suggestion)", () => {
    const events: ChatEvent[] = [
      toolUse("call_s2", SUGGEST_SESSION_TOOL, { title: "Add tests", prompt: "Write unit tests." }),
    ];
    const { segments } = buildSegments(events, true);
    const seg = segments[0] as Extract<Segment, { kind: "suggest_session" }>;
    expect(seg.kind).toBe("suggest_session");
    expect(seg.projectSlug).toBeUndefined();
  });
});

describe("buildSegments user_message suppression", () => {
  it("suppresses a still-sending message while the turn is in flight", () => {
    const { segments } = buildSegments([userMessage("typing…", "sending")], false);
    expect(segments).toHaveLength(0);
  });

  it("emits the message once the turn completes (sending → treated as delivered)", () => {
    const { segments } = buildSegments([userMessage("typing…", "sending")], true);
    expect(segments).toHaveLength(1);
    expect(segments[0]?.kind).toBe("user_message");
  });

  it("always emits a delivered message regardless of turn completion", () => {
    const { segments } = buildSegments([userMessage("done", "delivered")], false);
    expect(segments).toHaveLength(1);
  });
});

describe("buildSegments agent_message", () => {
  it("defaults direction to received and fills sender/target blanks", () => {
    const { segments } = buildSegments(
      [agentMessage({ content: "hello", senderName: "Lead", senderSessionId: "s1" })],
      true,
    );
    const seg = segments[0] as Extract<Segment, { kind: "agent_message" }>;
    expect(seg).toMatchObject({
      kind: "agent_message",
      direction: "received",
      content: "hello",
      senderName: "Lead",
      targetName: "",
      targetSessionId: "",
    });
  });
});

describe("buildTurnSections", () => {
  it("splits agent runs around each user message", () => {
    const { segments } = buildSegments(
      [
        text("intro"),
        userMessage("first ask", "delivered"),
        text("reply"),
        errorEvent("oops"),
        userMessage("second ask", "delivered"),
        text("final"),
      ],
      true,
    );
    const sections = buildTurnSections(segments);
    expect(sections.map((s) => s.kind)).toEqual(["agent", "user", "agent", "user", "agent"]);
    // The middle agent run holds both the reply text and the error.
    const middle = sections[2] as Extract<(typeof sections)[number], { kind: "agent" }>;
    expect(middle.items.map((i) => i.seg.kind)).toEqual(["text", "error"]);
  });

  it("returns a single agent section when there are no user messages", () => {
    const { segments } = buildSegments([text("a"), errorEvent("b")], true);
    const sections = buildTurnSections(segments);
    expect(sections).toHaveLength(1);
    expect(sections[0]?.kind).toBe("agent");
  });
});
