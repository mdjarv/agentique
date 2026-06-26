import { describe, expect, it } from "vitest";
import {
  extractContextUsageFromTurns,
  extractTodosFromEvent,
  extractTodosFromTurns,
  foldTaskList,
  isTaskListEvent,
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

const taskCreate = (toolId: string, subject: string, activeForm?: string): ChatEvent => ({
  id: rid(),
  type: "tool_use",
  toolId,
  toolName: "TaskCreate",
  toolInput: { subject, ...(activeForm ? { activeForm } : {}) },
});

// The harness assigns the task id and returns it in the result text.
const taskCreated = (toolId: string, id: number, subject = "x"): ChatEvent => ({
  id: rid(),
  type: "tool_result",
  toolId,
  contentBlocks: [{ type: "text", text: `Task #${id} created successfully: ${subject}` }],
});

const taskUpdate = (taskId: string, fields: Record<string, unknown>): ChatEvent => ({
  id: rid(),
  type: "tool_use",
  toolId: "",
  toolName: "TaskUpdate",
  toolInput: { taskId, ...fields },
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

describe("isTaskListEvent", () => {
  it("matches TodoWrite / TaskCreate / TaskUpdate tool_use, nothing else", () => {
    expect(isTaskListEvent(todoWrite([]))).toBe(true);
    expect(isTaskListEvent(taskCreate("t1", "do a thing"))).toBe(true);
    expect(isTaskListEvent(taskUpdate("1", { status: "completed" }))).toBe(true);
    expect(isTaskListEvent(taskCreated("t1", 1))).toBe(false); // results never trigger a recompute
    expect(
      isTaskListEvent({ id: rid(), type: "tool_use", toolId: "", toolName: "Bash", toolInput: {} }),
    ).toBe(false);
  });
});

describe("foldTaskList", () => {
  it("builds a list from TaskCreate, assigning ids from the result, then applies TaskUpdate", () => {
    const todos = foldTaskList([
      taskCreate("t1", "Build demo", "Building the demo"),
      taskCreated("t1", 1, "Build demo"),
      taskCreate("t2", "Write docs"),
      taskCreated("t2", 2, "Write docs"),
      taskUpdate("1", { status: "in_progress" }),
    ]);
    expect(todos).toEqual([
      { content: "Build demo", activeForm: "Building the demo", status: "in_progress" },
      { content: "Write docs", activeForm: undefined, status: "pending" },
    ]);
  });

  it("shows a created task as pending before its result/id has arrived", () => {
    // Live streaming: the TaskCreate tool_use lands before its tool_result.
    const todos = foldTaskList([taskCreate("t1", "Pending task")]);
    expect(todos).toEqual([{ content: "Pending task", activeForm: undefined, status: "pending" }]);
  });

  it("drops a task on status: deleted and updates subject/activeForm", () => {
    const todos = foldTaskList([
      taskCreate("t1", "Keep"),
      taskCreated("t1", 1),
      taskCreate("t2", "Remove"),
      taskCreated("t2", 2),
      taskUpdate("2", { status: "deleted" }),
      taskUpdate("1", { subject: "Kept renamed", activeForm: "Renaming", status: "in_progress" }),
    ]);
    expect(todos).toEqual([
      { content: "Kept renamed", activeForm: "Renaming", status: "in_progress" },
    ]);
  });

  it("ignores a TaskUpdate for a create it never saw", () => {
    expect(foldTaskList([taskUpdate("99", { status: "completed" })])).toBeNull();
  });

  it("ignores a stray result that matches no TaskCreate (no false task)", () => {
    expect(foldTaskList([taskCreated("nope", 5)])).toBeNull();
  });

  it("falls back to the latest TodoWrite snapshot when no Task* events exist", () => {
    const todos = foldTaskList([
      todoWrite([{ content: "old", status: "completed" }]),
      todoWrite([{ content: "new", status: "pending" }]),
    ]);
    expect(todos).toEqual([{ content: "new", activeForm: undefined, status: "pending" }]);
  });

  it("prefers the incremental task store over a legacy TodoWrite snapshot", () => {
    const todos = foldTaskList([
      todoWrite([{ content: "legacy", status: "pending" }]),
      taskCreate("t1", "modern"),
      taskCreated("t1", 1),
    ]);
    expect(todos).toEqual([{ content: "modern", activeForm: undefined, status: "pending" }]);
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
