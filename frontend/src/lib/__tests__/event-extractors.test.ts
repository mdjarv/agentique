import { describe, expect, it } from "vitest";
import {
  extractContextUsageFromTurns,
  extractTodosFromEvent,
  extractTodosFromTurns,
  parseTodoItems,
} from "~/lib/event-extractors";
import type { ChatEvent, Turn } from "~/stores/chat-types";

const rid = () => `id-${Math.random().toString(36).slice(2)}`;

function turn(events: ChatEvent[]): Turn {
  return { id: rid(), prompt: "", attachments: [], events, complete: true };
}

const todoWrite = (todos: unknown): ChatEvent => ({
  id: rid(),
  type: "tool_use",
  toolId: "",
  toolName: "TodoWrite",
  toolInput: { todos },
});

const resultEvent = (
  contextWindow?: number,
  inputTokens?: number,
  outputTokens?: number,
): ChatEvent => ({
  id: rid(),
  type: "result",
  contextWindow,
  inputTokens,
  outputTokens,
});

describe("parseTodoItems", () => {
  it("returns null for non-object / missing todos array", () => {
    expect(parseTodoItems(null)).toBeNull();
    expect(parseTodoItems("nope")).toBeNull();
    expect(parseTodoItems({})).toBeNull();
    expect(parseTodoItems({ todos: "x" })).toBeNull();
  });

  it("skips malformed items and keeps valid ones", () => {
    const items = parseTodoItems({
      todos: [
        { content: "valid", status: "pending" },
        { content: "no status" },
        { status: "missing content" },
        null,
        { content: "with form", status: "in_progress", activeForm: "doing" },
      ],
    });
    expect(items).toEqual([
      { content: "valid", activeForm: undefined, status: "pending" },
      { content: "with form", activeForm: "doing", status: "in_progress" },
    ]);
  });

  it("returns null when no valid item survives", () => {
    expect(parseTodoItems({ todos: [{ content: 1 }, {}] })).toBeNull();
  });
});

describe("extractTodosFromEvent", () => {
  it("only extracts from a TodoWrite tool_use", () => {
    expect(extractTodosFromEvent(todoWrite([{ content: "a", status: "pending" }]))).toHaveLength(1);
    expect(
      extractTodosFromEvent({
        id: rid(),
        type: "tool_use",
        toolId: "",
        toolName: "Bash",
        toolInput: {},
      }),
    ).toBeNull();
    expect(extractTodosFromEvent({ id: rid(), type: "text", content: "x" })).toBeNull();
  });
});

describe("extractTodosFromTurns", () => {
  it("returns the most recent todo list, scanning newest-first", () => {
    const turns = [
      turn([todoWrite([{ content: "old", status: "completed" }])]),
      turn([todoWrite([{ content: "new", status: "pending" }])]),
    ];
    expect(extractTodosFromTurns(turns)?.[0]?.content).toBe("new");
  });

  it("returns null when no turn has todos", () => {
    expect(extractTodosFromTurns([turn([{ id: rid(), type: "text", content: "x" }])])).toBeNull();
  });
});

describe("extractContextUsageFromTurns", () => {
  it("returns usage from the most recent result with a context window", () => {
    const turns = [turn([resultEvent(100_000, 10, 5)]), turn([resultEvent(200_000, 20, 10)])];
    expect(extractContextUsageFromTurns(turns)).toEqual({
      contextWindow: 200_000,
      inputTokens: 20,
      outputTokens: 10,
    });
  });

  it("ignores results with no/zero context window", () => {
    expect(extractContextUsageFromTurns([turn([resultEvent(0, 1, 1)])])).toBeNull();
    expect(extractContextUsageFromTurns([turn([resultEvent(undefined)])])).toBeNull();
  });

  it("short-circuits to null when a compact_boundary is newer than the last result", () => {
    // After a compaction the prior result's usage is stale — scanning
    // newest-first, the boundary is hit before the result and wins.
    const turns = [
      turn([resultEvent(200_000, 20, 10)]),
      turn([{ id: rid(), type: "compact_boundary" }]),
    ];
    expect(extractContextUsageFromTurns(turns)).toBeNull();
  });

  it("returns usage from a result that is newer than the compaction", () => {
    const turns = [
      turn([{ id: rid(), type: "compact_boundary" }]),
      turn([resultEvent(150_000, 7, 3)]),
    ];
    expect(extractContextUsageFromTurns(turns)).toEqual({
      contextWindow: 150_000,
      inputTokens: 7,
      outputTokens: 3,
    });
  });
});
