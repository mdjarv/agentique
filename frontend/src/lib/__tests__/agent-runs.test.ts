import { describe, expect, it } from "vitest";
import {
  type AgentRun,
  type AgentRunState,
  agentBadgeState,
  agentRunTotals,
  collectAgentRuns,
  flattenPreview,
  flightElapsedMs,
  oldestFlightElapsedMs,
  partitionAgentRuns,
} from "~/lib/agent-runs";
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

describe("partitionAgentRuns", () => {
  it("splits into still-out and came-back, landing newest first", () => {
    const runs = collectAgentRuns(
      [
        turn([
          spawn("tu_1", { description: "first" }),
          task("tu_1", { taskSubtype: "task_notification", taskStatus: "completed" }),
          spawn("tu_2", { description: "still out" }),
          task("tu_2", { taskSubtype: "task_started" }),
          spawn("tu_3", { description: "second" }),
          task("tu_3", { taskSubtype: "task_notification", taskStatus: "error" }),
        ]),
      ],
      undefined,
    );

    const { inFlight, landed } = partitionAgentRuns(runs);
    expect(inFlight.map((r) => r.title)).toEqual(["still out"]);
    // Reverse spawn order: the report you just asked for reads first.
    expect(landed.map((r) => r.title)).toEqual(["second", "first"]);
  });

  it("keeps in-flight runs in spawn order so the oldest reads first", () => {
    const runs = collectAgentRuns(
      [
        turn([
          spawn("tu_1", { description: "oldest" }),
          task("tu_1", { taskSubtype: "task_started" }),
          spawn("tu_2", { description: "newest" }),
          task("tu_2", { taskSubtype: "task_started" }),
        ]),
      ],
      undefined,
    );

    expect(partitionAgentRuns(runs).inFlight.map((r) => r.title)).toEqual(["oldest", "newest"]);
  });
});

describe("flight elapsed", () => {
  it("measures from the spawn timestamp when events carried one", () => {
    const runs = collectAgentRuns(
      [
        turn([
          spawn("tu_1", { description: "a" }),
          { ...task("tu_1", { taskSubtype: "task_started" }), timestamp: 1_000 } as ChatEvent,
        ]),
      ],
      undefined,
    );

    expect(flightElapsedMs(runs[0] as AgentRun, 4_000)).toBe(3_000);
  });

  it("falls back to the last reported duration without a timestamp", () => {
    const runs = collectAgentRuns(
      [
        turn([
          spawn("tu_1", { description: "a" }),
          task("tu_1", { taskSubtype: "task_progress", durationMs: 9_000 }),
        ]),
      ],
      undefined,
    );

    expect(flightElapsedMs(runs[0] as AgentRun, 4_000)).toBe(9_000);
  });

  it("reports the longest-running agent, which is the one that looks wedged", () => {
    const runs: AgentRun[] = [
      {
        toolUseId: "a",
        title: "a",
        state: "running",
        totalTokens: 0,
        toolUses: 0,
        durationMs: 0,
        startedAt: 5_000,
      },
      {
        toolUseId: "b",
        title: "b",
        state: "running",
        totalTokens: 0,
        toolUses: 0,
        durationMs: 0,
        startedAt: 1_000,
      },
    ];
    expect(oldestFlightElapsedMs(runs, 6_000)).toBe(5_000);
    expect(oldestFlightElapsedMs([], 6_000)).toBeUndefined();
  });
});

describe("agentBadgeState", () => {
  function runsWith(states: Array<[AgentRunState, number | undefined]>): AgentRun[] {
    return states.map(([state, turnIndex], i) => ({
      toolUseId: `tu_${i}`,
      title: `agent ${i}`,
      state,
      totalTokens: 0,
      toolUses: 0,
      durationMs: 0,
      turnIndex,
    }));
  }

  it("says nothing for a session whose agents all finished cleanly", () => {
    const runs = runsWith([
      ["done", 1],
      ["done", 2],
    ]);
    expect(agentBadgeState(runs, 2)).toEqual({ running: 0, failed: 0 });
  });

  it("counts agents still out, never the lifetime total", () => {
    const runs = runsWith([
      ["done", 1],
      ["done", 1],
      ["running", 2],
      ["running", 2],
    ]);
    expect(agentBadgeState(runs, 2).running).toBe(2);
  });

  it("raises failures from the latest turn", () => {
    const runs = runsWith([
      ["failed", 4],
      ["failed", 4],
      ["done", 4],
    ]);
    expect(agentBadgeState(runs, 4)).toEqual({ running: 0, failed: 2, failedTurn: 4 });
  });

  it("clears a failure once the session moves to a new turn", () => {
    const runs = runsWith([["failed", 4]]);
    expect(agentBadgeState(runs, 5).failed).toBe(0);
  });

  it("clears a failure once the tab has been opened on that turn", () => {
    const runs = runsWith([["failed", 4]]);
    expect(agentBadgeState(runs, 4, 4).failed).toBe(0);
    // A later turn failing again is a new fact, not the seen one.
    expect(agentBadgeState(runsWith([["failed", 5]]), 5, 4).failed).toBe(1);
  });

  it("attributes streaming runs to the turn they are part of", () => {
    const runs = runsWith([["failed", undefined]]);
    expect(agentBadgeState(runs, 7)).toEqual({ running: 0, failed: 1, failedTurn: 7 });
    expect(agentBadgeState(runs, 7, 7).failed).toBe(0);
  });

  it("only counts failures from the newest failing turn", () => {
    const runs = runsWith([
      ["failed", 3],
      ["failed", 4],
    ]);
    expect(agentBadgeState(runs, 4)).toEqual({ running: 0, failed: 1, failedTurn: 4 });
  });

  it("says nothing at all for a session with no agents", () => {
    expect(agentBadgeState([], 3)).toEqual({ running: 0, failed: 0 });
  });
});
