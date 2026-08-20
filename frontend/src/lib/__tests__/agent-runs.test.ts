import { describe, expect, it } from "vitest";
import { agentRunTotals, collectAgentRuns, flattenPreview } from "~/lib/agent-runs";
import type { ChatEvent, TaskEvent, Turn } from "~/stores/chat-types";

function spawn(toolId: string, input: unknown): ChatEvent {
  return { id: toolId, type: "tool_use", toolId, toolName: "Agent", toolInput: input };
}

function toolResult(toolId: string, text: string): ChatEvent {
  return {
    id: `${toolId}-r`,
    type: "tool_result",
    toolId,
    contentBlocks: [{ type: "text", text }],
  };
}

function task(toolUseId: string, overrides: Partial<TaskEvent> = {}): ChatEvent {
  return {
    id: `${toolUseId}-${overrides.taskSubtype ?? "t"}-${Math.random()}`,
    type: "task",
    toolUseId,
    taskId: undefined,
    ...overrides,
  } as TaskEvent;
}

function turn(events: ChatEvent[], turnIndex?: number): Turn {
  return {
    id: `turn-${turnIndex ?? 0}`,
    prompt: "",
    attachments: [],
    events,
    complete: true,
    turnIndex,
  };
}

describe("collectAgentRuns", () => {
  it("folds spawn, task lifecycle, and result into one run", () => {
    const runs = collectAgentRuns(
      [
        turn(
          [
            spawn("tu_1", { description: "Explore auth", subagent_type: "Explore" }),
            task("tu_1", { taskSubtype: "task_started", taskDescription: "Explore auth" }),
            task("tu_1", {
              taskSubtype: "task_notification",
              taskStatus: "completed",
              taskSummary: "Auth lives in\n  internal/auth",
              totalTokens: 245_000,
              toolUses: 91,
              durationMs: 397_000,
            }),
            toolResult("tu_1", "ignored when a summary exists"),
          ],
          4,
        ),
      ],
      undefined,
    );

    expect(runs).toEqual([
      {
        toolUseId: "tu_1",
        title: "Explore auth",
        agentType: "Explore",
        state: "done",
        preview: "Auth lives in internal/auth",
        lastToolName: undefined,
        totalTokens: 245_000,
        toolUses: 91,
        durationMs: 397_000,
        turnIndex: 4,
      },
    ]);
  });

  it("falls back to the tool result when no summary was reported", () => {
    const runs = collectAgentRuns(
      [
        turn([
          spawn("tu_1", { description: "Map the codebase" }),
          task("tu_1", { taskSubtype: "task_started" }),
          toolResult("tu_1", "Here is the report.\n\n# Findings\nThe cache is unbounded."),
        ]),
      ],
      undefined,
    );

    expect(runs[0]?.state).toBe("done");
    expect(runs[0]?.preview).toBe("Here is the report. # Findings The cache is unbounded.");
  });

  it("reports a running agent with its live tool, and no preview", () => {
    const runs = collectAgentRuns(
      [
        turn([
          spawn("tu_1", { description: "Sweep for leaks" }),
          task("tu_1", { taskSubtype: "task_started" }),
          task("tu_1", { taskSubtype: "task_progress", lastToolName: "Grep", toolUses: 12 }),
        ]),
      ],
      undefined,
    );

    expect(runs[0]).toMatchObject({
      state: "running",
      preview: undefined,
      lastToolName: "Grep",
      toolUses: 12,
    });
  });

  it("treats a non-completed terminal status as failed", () => {
    const runs = collectAgentRuns(
      [
        turn([
          spawn("tu_1", { description: "Broken" }),
          task("tu_1", { taskSubtype: "task_notification", taskStatus: "error" }),
        ]),
      ],
      undefined,
    );

    expect(runs[0]?.state).toBe("failed");
  });

  it("keeps metrics from the newest event that carries them", () => {
    const runs = collectAgentRuns(
      [
        turn([
          spawn("tu_1", { description: "Counting" }),
          task("tu_1", { taskSubtype: "task_progress", totalTokens: 100, toolUses: 2 }),
          task("tu_1", { taskSubtype: "task_progress", totalTokens: 900 }),
          task("tu_1", { taskSubtype: "task_notification", taskStatus: "completed" }),
        ]),
      ],
      undefined,
    );

    // toolUses is not re-reported by the later events, so the last real value wins.
    expect(runs[0]).toMatchObject({ totalTokens: 900, toolUses: 2 });
  });

  it("excludes workflow tasks from the roster", () => {
    const runs = collectAgentRuns(
      [
        turn([
          spawn("tu_wf", { description: "Run the workflow" }),
          task("tu_wf", { taskSubtype: "task_started", taskType: "local_workflow" }),
          spawn("tu_1", { description: "A real agent" }),
          task("tu_1", { taskSubtype: "task_started" }),
        ]),
      ],
      undefined,
    );

    expect(runs.map((r) => r.toolUseId)).toEqual(["tu_1"]);
  });

  it("includes agents still streaming in the live turn, spawn order preserved", () => {
    const runs = collectAgentRuns(
      [
        turn([
          spawn("tu_1", { description: "First" }),
          task("tu_1", { taskSubtype: "task_started" }),
        ]),
      ],
      [spawn("tu_2", { description: "Second" }), task("tu_2", { taskSubtype: "task_started" })],
    );

    expect(runs.map((r) => r.title)).toEqual(["First", "Second"]);
  });

  it("falls back through description, prompt, then a generic label", () => {
    const runs = collectAgentRuns(
      [
        turn([
          spawn("tu_1", { prompt: "Find the bug" }),
          task("tu_1", { taskSubtype: "task_started" }),
          spawn("tu_2", {}),
          task("tu_2", { taskSubtype: "task_started" }),
        ]),
      ],
      undefined,
    );

    expect(runs.map((r) => r.title)).toEqual(["Find the bug", "Agent"]);
  });

  it("returns an empty roster for a session that spawned nothing", () => {
    expect(collectAgentRuns(undefined, undefined)).toEqual([]);
    expect(collectAgentRuns([turn([{ id: "t", type: "text", content: "hi" }])], [])).toEqual([]);
  });
});

describe("agentRunTotals", () => {
  it("counts states and sums tokens", () => {
    const runs = collectAgentRuns(
      [
        turn([
          spawn("tu_1", { description: "a" }),
          task("tu_1", {
            taskSubtype: "task_notification",
            taskStatus: "completed",
            totalTokens: 10,
          }),
          spawn("tu_2", { description: "b" }),
          task("tu_2", { taskSubtype: "task_notification", taskStatus: "error", totalTokens: 5 }),
          spawn("tu_3", { description: "c" }),
          task("tu_3", { taskSubtype: "task_started" }),
        ]),
      ],
      undefined,
    );

    expect(agentRunTotals(runs)).toEqual({ running: 1, done: 1, failed: 1, totalTokens: 15 });
  });
});

describe("flattenPreview", () => {
  it("collapses all whitespace runs to single spaces", () => {
    expect(flattenPreview("  a\n\n  b\t c  ")).toBe("a b c");
  });
});
