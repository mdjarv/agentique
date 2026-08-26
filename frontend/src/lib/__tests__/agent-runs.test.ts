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
  scopeAgentRuns,
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
        report: "ignored when a summary exists",
        steps: [],
        lastToolName: undefined,
        totalTokens: 245_000,
        toolUses: 91,
        durationMs: 397_000,
        turnIndex: 4,
      },
    ]);
  });

  it("keeps the report whole, and flattens only the preview", () => {
    const runs = collectAgentRuns(
      [
        turn([
          spawn("tu_1", { description: "Map the codebase" }),
          task("tu_1", { taskSubtype: "task_started" }),
          toolResult("tu_1", "# Findings\n\nThe cache is unbounded."),
        ]),
      ],
      undefined,
    );

    expect(runs[0]?.report).toBe("# Findings\n\nThe cache is unbounded.");
    expect(runs[0]?.preview).toBe("# Findings The cache is unbounded.");
  });

  it("collects forwarded subagent output under the agent that produced it", () => {
    const narration: ChatEvent[] = [
      { id: "n1", type: "thinking", content: "Start at the reaper.", parentToolUseId: "tu_1" },
      {
        id: "n2",
        type: "tool_use",
        toolId: "sub_1",
        toolName: "Grep",
        toolInput: null,
        parentToolUseId: "tu_1",
      },
      { id: "n3", type: "text", content: "Found it.", parentToolUseId: "tu_2" },
    ];
    const runs = collectAgentRuns(
      [
        turn([
          spawn("tu_1", { description: "First" }),
          task("tu_1", { taskSubtype: "task_started" }),
          spawn("tu_2", { description: "Second" }),
          task("tu_2", { taskSubtype: "task_started" }),
          ...narration,
        ]),
      ],
      undefined,
    );

    expect(runs[0]?.steps.map((e) => e.id)).toEqual(["n1", "n2"]);
    expect(runs[1]?.steps.map((e) => e.id)).toEqual(["n3"]);
  });

  it("gives a running agent its narration, which is the only thing it has", () => {
    const runs = collectAgentRuns(
      [
        turn([
          spawn("tu_1", { description: "Still out" }),
          task("tu_1", { taskSubtype: "task_started" }),
        ]),
      ],
      [{ id: "n1", type: "text", content: "Reading the pipeline.", parentToolUseId: "tu_1" }],
    );

    expect(runs[0]).toMatchObject({ state: "running", preview: undefined, report: undefined });
    expect(runs[0]?.steps).toHaveLength(1);
  });

  it("never mistakes a subagent's own tool call for a spawn or a return value", () => {
    const runs = collectAgentRuns(
      [
        turn([
          spawn("tu_1", { description: "Parent" }),
          task("tu_1", { taskSubtype: "task_started" }),
          // The subagent spawns nothing and returns nothing to this session:
          // both events belong to its narration.
          {
            id: "n1",
            type: "tool_use",
            toolId: "tu_nested",
            toolName: "Agent",
            toolInput: { description: "Nested" },
            parentToolUseId: "tu_1",
          },
          {
            id: "n2",
            type: "tool_result",
            toolId: "tu_1",
            contentBlocks: [{ type: "text", text: "not the agent's report" }],
            parentToolUseId: "tu_1",
          },
          task("tu_1", { taskSubtype: "task_notification", taskStatus: "completed" }),
        ]),
      ],
      undefined,
    );

    expect(runs.map((r) => r.toolUseId)).toEqual(["tu_1"]);
    expect(runs[0]?.report).toBeUndefined();
    expect(runs[0]?.steps.map((e) => e.id)).toEqual(["n1", "n2"]);
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

  it("treats an error status as failed", () => {
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

  it.each(["stopped", "killed", "cancelled", "canceled", "aborted"])(
    "reports %s as stopped, not failed",
    (status) => {
      const runs = collectAgentRuns(
        [
          turn([
            spawn("tu_1", { description: "Shut down on purpose" }),
            task("tu_1", { taskSubtype: "task_notification", taskStatus: status }),
          ]),
        ],
        undefined,
      );

      expect(runs[0]?.state).toBe("stopped");
    },
  );

  it("drops a preview that only repeats the title", () => {
    // Background task streams set `summary` to the description verbatim; a row
    // that prints itself twice is noise, not an outcome.
    const runs = collectAgentRuns(
      [
        turn([
          spawn("tu_1", { description: "Run make check" }),
          task("tu_1", {
            taskSubtype: "task_notification",
            taskStatus: "completed",
            taskSummary: "Run make check",
          }),
        ]),
      ],
      undefined,
    );

    expect(runs[0]?.title).toBe("Run make check");
    expect(runs[0]?.preview).toBeUndefined();
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

  it("keeps the exclusion when only task_started carried the type", () => {
    // Older CLIs stamp `taskType` on task_started and leave it empty on every
    // later event. Judged per event, the terminal notification slips through
    // and invents a roster row for a workflow the workflow panel already shows.
    const runs = collectAgentRuns(
      [
        turn([
          spawn("tu_wf", { description: "Run the workflow" }),
          task("tu_wf", { taskSubtype: "task_started", taskType: "local_workflow" }),
          task("tu_wf", { taskSubtype: "task_notification", taskStatus: "completed" }),
        ]),
      ],
      undefined,
    );

    expect(runs).toEqual([]);
  });

  it("excludes backgrounded shell commands", () => {
    // A long session runs dozens of these and no subagents at all; they are the
    // transcript's business, not the roster's.
    const bash: ChatEvent = {
      id: "tu_bash",
      type: "tool_use",
      toolId: "tu_bash",
      toolName: "Bash",
      toolInput: { command: "make check", description: "Run make check" },
    };
    const runs = collectAgentRuns(
      [
        turn([
          bash,
          task("tu_bash", {
            taskSubtype: "task_started",
            taskType: "local_bash",
            taskDescription: "Run make check",
          }),
          task("tu_bash", { taskSubtype: "task_notification", taskStatus: "killed" }),
          spawn("tu_1", { description: "A real agent" }),
          task("tu_1", { taskSubtype: "task_started", taskType: "local_agent" }),
        ]),
      ],
      undefined,
    );

    expect(runs.map((r) => r.toolUseId)).toEqual(["tu_1"]);
  });

  it("admits a typeless task whose spawn is an Agent call", () => {
    const runs = collectAgentRuns(
      [
        turn([
          spawn("tu_1", { description: "No taskType anywhere" }),
          task("tu_1", { taskSubtype: "task_started" }),
        ]),
      ],
      undefined,
    );

    expect(runs.map((r) => r.toolUseId)).toEqual(["tu_1"]);
  });

  it("excludes a task with neither a type nor a known spawn", () => {
    const runs = collectAgentRuns([turn([task("tu_orphan", { taskSubtype: "task_started" })])], []);

    expect(runs).toEqual([]);
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

    expect(agentRunTotals(runs)).toEqual({
      running: 1,
      done: 1,
      failed: 1,
      stopped: 0,
      totalTokens: 15,
    });
  });

  it("counts stopped runs apart from failed ones", () => {
    const runs = collectAgentRuns(
      [
        turn([
          spawn("tu_1", { description: "a" }),
          task("tu_1", { taskSubtype: "task_notification", taskStatus: "killed" }),
        ]),
      ],
      undefined,
    );

    expect(agentRunTotals(runs)).toMatchObject({ stopped: 1, failed: 0, done: 0 });
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

describe("scopeAgentRuns", () => {
  const threeTurns = () =>
    collectAgentRuns(
      [
        turn(
          [
            spawn("tu_1", { description: "long ago" }),
            task("tu_1", { taskSubtype: "task_notification", taskStatus: "completed" }),
          ],
          1,
        ),
        turn(
          [
            spawn("tu_2", { description: "last turn" }),
            task("tu_2", { taskSubtype: "task_notification", taskStatus: "completed" }),
          ],
          2,
        ),
        turn(
          [
            spawn("tu_3", { description: "this turn" }),
            task("tu_3", { taskSubtype: "task_notification", taskStatus: "completed" }),
            spawn("tu_4", { description: "still out" }),
            task("tu_4", { taskSubtype: "task_started" }),
          ],
          3,
        ),
      ],
      undefined,
    );

  it("shows this turn and folds every older run away", () => {
    const { inFlight, landed, earlier } = scopeAgentRuns(threeTurns(), 3);
    expect(inFlight.map((r) => r.title)).toEqual(["still out"]);
    expect(landed.map((r) => r.title)).toEqual(["this turn"]);
    expect(earlier.map((r) => r.title)).toEqual(["last turn", "long ago"]);
  });

  it("folds nothing away without a turn to scope to", () => {
    const { landed, earlier } = scopeAgentRuns(threeTurns(), undefined);
    expect(landed).toHaveLength(3);
    expect(earlier).toEqual([]);
  });

  it("attributes a still-streaming run with no turn index to the current turn", () => {
    // Live runs carry no turnIndex until the turn is persisted; putting them
    // in `earlier` would fold away the very agents the dock is for.
    const runs = collectAgentRuns(undefined, [
      spawn("tu_live", { description: "streaming" }),
      task("tu_live", { taskSubtype: "task_notification", taskStatus: "completed" }),
    ]);
    const { landed, earlier } = scopeAgentRuns(runs, 7);
    expect(landed.map((r) => r.title)).toEqual(["streaming"]);
    expect(earlier).toEqual([]);
  });

  it("keeps earlier runs newest-first, like the roster they came from", () => {
    const { earlier } = scopeAgentRuns(threeTurns(), 3);
    expect(earlier.map((r) => r.title)).toEqual(["last turn", "long ago"]);
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
        steps: [],
        startedAt: 5_000,
      },
      {
        toolUseId: "b",
        title: "b",
        state: "running",
        totalTokens: 0,
        toolUses: 0,
        durationMs: 0,
        steps: [],
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
      steps: [],
      turnIndex,
    }));
  }

  it("says nothing for a session whose agents all finished cleanly", () => {
    const runs = runsWith([
      ["done", 1],
      ["done", 2],
    ]);
    expect(agentBadgeState(runs)).toEqual({ running: 0 });
  });

  it("counts agents still out, never the lifetime total", () => {
    const runs = runsWith([
      ["done", 1],
      ["done", 1],
      ["running", 2],
      ["running", 2],
    ]);
    expect(agentBadgeState(runs).running).toBe(2);
  });

  it("stays silent about a failed subagent, which the session handles itself", () => {
    const runs = runsWith([
      ["failed", 4],
      ["failed", 4],
      ["done", 4],
    ]);
    expect(agentBadgeState(runs)).toEqual({ running: 0 });
  });

  it("stays silent about a stopped subagent", () => {
    expect(agentBadgeState(runsWith([["stopped", 4]]))).toEqual({ running: 0 });
  });

  it("still counts an agent that is out while another failed", () => {
    const runs = runsWith([
      ["failed", 4],
      ["running", 4],
    ]);
    expect(agentBadgeState(runs)).toEqual({ running: 1 });
  });

  it("says nothing at all for a session with no agents", () => {
    expect(agentBadgeState([])).toEqual({ running: 0 });
  });
});
