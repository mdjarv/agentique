import type {
  ChatEvent,
  TaskEvent,
  ToolResultEvent,
  ToolUseEvent,
  Turn,
} from "~/stores/chat-types";

export type AgentRunState = "running" | "done" | "failed";

/**
 * One subagent spawned by this session, folded from the three event streams
 * that describe it: the parent `Agent` tool-use (title + type), its task
 * lifecycle events (status, tokens, tools, duration), and its tool result
 * (what it actually returned).
 *
 * The point of the roster is the *outcome*: `preview` carries the head of the
 * agent's return value, so a reader can tell what four agents concluded
 * without opening any of them.
 */
export interface AgentRun {
  /** Parent `Agent` tool-use id — stable identity across all three streams. */
  toolUseId: string;
  title: string;
  /** `subagent_type` from the spawn call, e.g. "Explore". */
  agentType?: string;
  state: AgentRunState;
  /** Flattened head of the return value. Absent while the agent is running. */
  preview?: string;
  lastToolName?: string;
  totalTokens: number;
  toolUses: number;
  durationMs: number;
  /**
   * Wall-clock start, from the spawn event. Present only when the events
   * carried a timestamp; a running agent without one falls back to the last
   * reported `durationMs`, which freezes between task_progress events.
   */
  startedAt?: number;
  /** Persisted turn index the agent was spawned in, when known. */
  turnIndex?: number;
}

/** Tool names that spawn a subagent. Providers disagree on the label. */
const AGENT_TOOL_NAMES = new Set(["Agent", "Task"]);

function readInput(input: unknown): Record<string, unknown> {
  return input && typeof input === "object" ? (input as Record<string, unknown>) : {};
}

function asText(value: unknown): string | undefined {
  return typeof value === "string" && value.trim() !== "" ? value : undefined;
}

/**
 * Collapse a multi-line agent report to a single scannable line. Newlines
 * become spaces rather than being cut at the first one: reports routinely open
 * with a throwaway sentence ("Here is the report.") and the signal sits in what
 * follows, so flattening surfaces more of it than truncating at the first break.
 */
export function flattenPreview(text: string): string {
  return text.replace(/\s+/gu, " ").trim();
}

function latestNumber(tasks: readonly TaskEvent[], pick: (t: TaskEvent) => number | undefined) {
  const hit = tasks.findLast((t) => {
    const value = pick(t);
    return value !== undefined && value > 0;
  });
  return (hit ? pick(hit) : undefined) ?? 0;
}

function latestString(tasks: readonly TaskEvent[], pick: (t: TaskEvent) => string | undefined) {
  const hit = tasks.findLast((t) => asText(pick(t)) !== undefined);
  return hit ? asText(pick(hit)) : undefined;
}

function resultText(result: ToolResultEvent | undefined): string | undefined {
  for (const block of result?.contentBlocks ?? []) {
    const text = asText(block.text);
    if (block.type === "text" && text) return flattenPreview(text);
  }
  return undefined;
}

function runState(tasks: readonly TaskEvent[], hasResult: boolean): AgentRunState {
  const status = tasks.findLast((t) => t.taskSubtype === "task_notification")?.taskStatus;
  if (status === "completed" || status === "success") return "done";
  if (status !== undefined && status !== "in_progress") return "failed";
  // A tool result without a notification still means the spawn returned — the
  // parent agent cannot continue until it does.
  return hasResult ? "done" : "running";
}

/**
 * Fold a session's events into its subagent roster, oldest spawn first.
 * Workflow tasks are excluded — they have their own panel, and the single
 * synthetic task representing a workflow would otherwise sit in the roster
 * pretending to be an agent.
 *
 * Callers should `useMemo` this over the session's `turns` + `streamingEvents`
 * (both referentially stable between store updates) — never call it inside a
 * Zustand selector, which would return a fresh array every render.
 */
export function collectAgentRuns(
  turns: Turn[] | undefined,
  streamingEvents: ChatEvent[] | undefined,
): AgentRun[] {
  const spawns = new Map<string, { use: ToolUseEvent; turnIndex?: number }>();
  const results = new Map<string, ToolResultEvent>();
  const tasksByToolUse = new Map<string, TaskEvent[]>();
  const order: string[] = [];

  const consider = (ev: ChatEvent, turnIndex?: number) => {
    if (ev.type === "tool_use" && AGENT_TOOL_NAMES.has(ev.toolName)) {
      spawns.set(ev.toolId, { use: ev, turnIndex });
      return;
    }
    if (ev.type === "tool_result") {
      results.set(ev.toolId, ev);
      return;
    }
    if (ev.type !== "task" || !ev.toolUseId) return;
    if (ev.taskType === "local_workflow") return;
    let list = tasksByToolUse.get(ev.toolUseId);
    if (!list) {
      list = [];
      tasksByToolUse.set(ev.toolUseId, list);
      order.push(ev.toolUseId);
    }
    list.push(ev);
  };

  for (const turn of turns ?? []) {
    for (const ev of turn.events) consider(ev, turn.turnIndex);
  }
  for (const ev of streamingEvents ?? []) consider(ev);

  const runs: AgentRun[] = [];
  for (const toolUseId of order) {
    const tasks = tasksByToolUse.get(toolUseId) ?? [];
    const spawn = spawns.get(toolUseId);
    const input = readInput(spawn?.use.toolInput);
    const started = tasks.find((t) => t.taskSubtype === "task_started");
    const result = results.get(toolUseId);
    const state = runState(tasks, result !== undefined);
    const summary = latestString(tasks, (t) => t.taskSummary);

    const title =
      asText(started?.taskDescription) ??
      asText(input.description) ??
      asText(input.prompt) ??
      "Agent";

    runs.push({
      toolUseId,
      title: flattenPreview(title),
      agentType: asText(input.subagent_type),
      state,
      preview:
        state === "running" ? undefined : summary ? flattenPreview(summary) : resultText(result),
      lastToolName: latestString(tasks, (t) => t.lastToolName),
      totalTokens: latestNumber(tasks, (t) => t.totalTokens),
      toolUses: latestNumber(tasks, (t) => t.toolUses),
      durationMs: latestNumber(tasks, (t) => t.durationMs),
      startedAt: started?.timestamp ?? spawn?.use.timestamp,
      turnIndex: spawn?.turnIndex,
    });
  }
  return runs;
}

export interface AgentRunPartition {
  /** Still out, oldest spawn first — the one that has been gone longest reads first. */
  inFlight: AgentRun[];
  /** Returned, newest first: the reports you just asked for are at the top. */
  landed: AgentRun[];
}

/**
 * Split the roster into the only two groups the panel shows. Deliberately not
 * grouped by turn: "still out" and "came back" are the two states a reader
 * acts on, and a turn boundary is not one of them.
 */
export function partitionAgentRuns(runs: readonly AgentRun[]): AgentRunPartition {
  const inFlight: AgentRun[] = [];
  const landed: AgentRun[] = [];
  for (const run of runs) {
    if (run.state === "running") inFlight.push(run);
    else landed.unshift(run);
  }
  return { inFlight, landed };
}

/** Elapsed wall-clock for a run that is still out, or `undefined` if unknowable. */
export function flightElapsedMs(run: AgentRun, now: number): number | undefined {
  if (run.startedAt !== undefined) return Math.max(0, now - run.startedAt);
  return run.durationMs > 0 ? run.durationMs : undefined;
}

/** Longest-running agent's elapsed — the number that says "something is wedged". */
export function oldestFlightElapsedMs(
  inFlight: readonly AgentRun[],
  now: number,
): number | undefined {
  let longest: number | undefined;
  for (const run of inFlight) {
    const elapsed = flightElapsedMs(run, now);
    if (elapsed !== undefined && (longest === undefined || elapsed > longest)) longest = elapsed;
  }
  return longest;
}

export interface AgentBadgeState {
  running: number;
  /**
   * Failures the badge should still be raising. Zero unless they are in the
   * session's latest turn and the user has not opened the tab since.
   */
  failed: number;
  /** Turn the live failures belong to — feed back as `seenTurn` once seen. */
  failedTurn?: number;
}

const NO_BADGE: AgentBadgeState = { running: 0, failed: 0 };

/**
 * What the Agents tab badge should say right now.
 *
 * A lifetime count is never the answer: the badge is a claim on attention, so
 * it carries only the two facts that can still be acted on — agents out, and
 * failures from the current turn. A failure marker clears on whichever comes
 * first, the user opening the tab (`seenTurn`) or the session moving to a new
 * turn; that is why both indices are parameters rather than derived here.
 *
 * Runs from the live stream have no `turnIndex` yet, so they are attributed to
 * `latestTurnIndex` — the turn they are, in fact, part of.
 */
export function agentBadgeState(
  runs: readonly AgentRun[],
  latestTurnIndex?: number,
  seenTurn?: number,
): AgentBadgeState {
  if (runs.length === 0) return NO_BADGE;

  let running = 0;
  let failedTurn: number | undefined;
  let failed = 0;

  for (const run of runs) {
    if (run.state === "running") {
      running += 1;
      continue;
    }
    if (run.state !== "failed") continue;
    const turn = run.turnIndex ?? latestTurnIndex ?? 0;
    if (failedTurn === undefined || turn > failedTurn) {
      failedTurn = turn;
      failed = 1;
    } else if (turn === failedTurn) {
      failed += 1;
    }
  }

  if (failedTurn === undefined) return { running, failed: 0 };

  const stale = failedTurn < (latestTurnIndex ?? failedTurn);
  if (stale || failedTurn === seenTurn) return { running, failed: 0 };
  return { running, failed, failedTurn };
}

export interface AgentRunTotals {
  running: number;
  done: number;
  failed: number;
  totalTokens: number;
}

export function agentRunTotals(runs: readonly AgentRun[]): AgentRunTotals {
  const totals: AgentRunTotals = { running: 0, done: 0, failed: 0, totalTokens: 0 };
  for (const run of runs) {
    totals[run.state] += 1;
    totals.totalTokens += run.totalTokens;
  }
  return totals;
}
