import type {
  AgentResultEvent,
  ChatEvent,
  TaskEvent,
  ToolResultEvent,
  ToolUseEvent,
  Turn,
} from "~/stores/chat-types";

/**
 * What became of a run.
 *
 * `stopped` is deliberately not `failed`: the CLI reports `stopped`/`killed`
 * when *the agent itself* shut a run down — the dev server it started for a
 * screenshot, the tail it no longer needs. Nothing went wrong, and painting it
 * red taught the reader to ignore red.
 */
export type AgentRunState = "running" | "done" | "failed" | "stopped";

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
  /**
   * What the agent returned, whole and unflattened — the report behind the
   * one-line preview. Absent while it is still out.
   *
   * Read from the `agent_result` event when one joins (see `collectAgentRuns`),
   * because the `Agent` tool call's result is truncated for DB storage and a
   * long report comes back from history with its middle missing.
   */
  report?: string;
  /**
   * The agent's own forwarded narration (text, thinking, tool calls), oldest
   * first: the only way to see what it is doing before it returns.
   *
   * Empty unless the provider forwards subagent output — Claude with
   * `[claude] forward-subagent-text`. Without it the CLI emits no subagent
   * messages at all, so this stays empty and the roster can only report status.
   */
  steps: ChatEvent[];
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

/**
 * The CLI carries three unrelated things on one `task` stream — a subagent, a
 * backgrounded shell command, and a workflow — and `taskType` is the only thing
 * that tells them apart. A session that ran `make check` in the background
 * forty times has forty of these; none of them is an agent.
 */
const AGENT_TASK_TYPE = "local_agent";
const NON_AGENT_TASK_TYPES = new Set(["local_bash", "local_workflow"]);

/**
 * Whether a run belongs in the subagent roster, judged once per run rather than
 * per event.
 *
 * Both signals are needed. `taskType` is stamped on `task_started` but older
 * CLIs leave it empty on every later event, so a rule applied per event lets a
 * workflow's terminal notification through and invents a row for it. And the
 * spawning tool call is the ground truth when a task carries no type at all:
 * a background shell's task points at a `Bash` call, an agent's at `Agent`.
 *
 * Unknown on both counts means excluded. Every `task_started` the CLI writes
 * today carries a type, so the ambiguous case is old history — and a stray
 * background command is a worse row than a missing one.
 */
function isSubagentRun(taskType: string | undefined, toolName: string | undefined): boolean {
  if (taskType === AGENT_TASK_TYPE) return true;
  if (taskType !== undefined && NON_AGENT_TASK_TYPES.has(taskType)) return false;
  return toolName !== undefined && AGENT_TOOL_NAMES.has(toolName);
}

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

/** The return value as written — line breaks and all. */
function resultText(result: ToolResultEvent | AgentResultEvent | undefined): string | undefined {
  for (const block of result?.contentBlocks ?? []) {
    const text = asText(block.text);
    if (block.type === "text" && text) return text.trim();
  }
  return undefined;
}

const DONE_STATUSES = new Set(["completed", "success"]);
/** Shut down on purpose — an outcome, but never an incident. */
const STOPPED_STATUSES = new Set(["stopped", "killed", "cancelled", "canceled", "aborted"]);

function runState(
  tasks: readonly TaskEvent[],
  hasResult: boolean,
  asyncLaunched: boolean,
): AgentRunState {
  // Any task event may carry the terminal status: `task_updated` reports it
  // (completed/killed/failed) and can land without a notification behind it.
  const status = tasks.findLast((t) => t.taskStatus !== undefined)?.taskStatus;
  if (status !== undefined && DONE_STATUSES.has(status)) return "done";
  if (status !== undefined && STOPPED_STATUSES.has(status)) return "stopped";
  if (status !== undefined && status !== "in_progress") return "failed";
  // A background agent's spawn returns immediately with a launch receipt, so
  // its tool_result is not the return — the run ends only when the task stream
  // says so. `agent_result: async_launched` is what marks that spawn.
  if (asyncLaunched) return "running";
  // A synchronous spawn's tool result means the run returned — the parent
  // agent cannot continue until it does.
  return hasResult ? "done" : "running";
}

/**
 * Fold a session's events into its subagent roster, oldest spawn first.
 *
 * Only subagents. Background shell commands and workflows ride the same task
 * stream and are dropped by `isSubagentRun` — the first because they are the
 * transcript's business and outnumber real agents roughly forty to one, the
 * second because `WorkflowActivity` already renders them properly.
 *
 * Callers should `useMemo` this over the session's `turns` + `streamingEvents`
 * (both referentially stable between store updates) — never call it inside a
 * Zustand selector, which would return a fresh array every render.
 */
export function collectAgentRuns(
  turns: Turn[] | undefined,
  streamingEvents: ChatEvent[] | undefined,
): AgentRun[] {
  // Every top-level tool call, not only the spawns: a task's tool name is half
  // of what decides whether the task is an agent at all.
  const spawns = new Map<string, { use: ToolUseEvent; turnIndex?: number }>();
  // First non-empty `taskType` seen for a run. Sticky, because only
  // `task_started` reliably carries one.
  const taskTypeByToolUse = new Map<string, string>();
  const results = new Map<string, ToolResultEvent>();
  const tasksByToolUse = new Map<string, TaskEvent[]>();
  const stepsByToolUse = new Map<string, ChatEvent[]>();
  // agent_result carries an empty parentToolUseId, so it cannot address its
  // spawn directly. It carries `agentId`, which is the task stream's `taskId`,
  // and a task event names the spawning tool call — so these two maps compose
  // into the join: agentId → taskId → toolUseId.
  const agentResultsByAgentId = new Map<string, AgentResultEvent>();
  const toolUseIdByTaskId = new Map<string, string>();
  const order: string[] = [];

  const consider = (ev: ChatEvent, turnIndex?: number) => {
    // An outcome, never narration: keep it out of `steps` even when it arrives
    // addressed to a parent, which a nested one does.
    if (ev.type === "agent_result") {
      if (ev.agentId) agentResultsByAgentId.set(ev.agentId, ev);
      return;
    }
    // Forwarded subagent output — the agent's own narration, addressed to the
    // spawn it belongs to. Collected first: a subagent's tool call is not a
    // spawn of the parent session, and its tool result is not the agent's
    // return value.
    if (ev.parentToolUseId && ev.type !== "task") {
      let steps = stepsByToolUse.get(ev.parentToolUseId);
      if (!steps) {
        steps = [];
        stepsByToolUse.set(ev.parentToolUseId, steps);
      }
      steps.push(ev);
      return;
    }
    if (ev.type === "tool_use") {
      spawns.set(ev.toolId, { use: ev, turnIndex });
      return;
    }
    if (ev.type === "tool_result") {
      results.set(ev.toolId, ev);
      return;
    }
    if (ev.type !== "task" || !ev.toolUseId) return;
    if (ev.taskType && !taskTypeByToolUse.has(ev.toolUseId)) {
      taskTypeByToolUse.set(ev.toolUseId, ev.taskType);
    }
    if (ev.taskId) toolUseIdByTaskId.set(ev.taskId, ev.toolUseId);
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

  // Resolve the join once, into the roster's own key.
  const agentReportByToolUse = new Map<string, string>();
  // Spawns whose tool_result was a launch receipt, not the agent's return:
  // an `agent_result` with status async_launched marks the run as backgrounded.
  const asyncByToolUse = new Set<string>();
  for (const [agentId, ev] of agentResultsByAgentId) {
    const toolUseId = toolUseIdByTaskId.get(agentId);
    if (!toolUseId) continue;
    if (ev.status === "async_launched") asyncByToolUse.add(toolUseId);
    const text = resultText(ev);
    if (text) agentReportByToolUse.set(toolUseId, text);
  }

  const runs: AgentRun[] = [];
  for (const toolUseId of order) {
    const tasks = tasksByToolUse.get(toolUseId) ?? [];
    const spawn = spawns.get(toolUseId);
    if (!isSubagentRun(taskTypeByToolUse.get(toolUseId), spawn?.use.toolName)) continue;
    const input = readInput(spawn?.use.toolInput);
    const started = tasks.find((t) => t.taskSubtype === "task_started");
    const result = results.get(toolUseId);
    const asyncLaunched = asyncByToolUse.has(toolUseId);
    const state = runState(tasks, result !== undefined, asyncLaunched);
    const summary = latestString(tasks, (t) => t.taskSummary);

    const title =
      asText(started?.taskDescription) ??
      asText(input.description) ??
      asText(input.prompt) ??
      "Agent";

    // Prefer the agent_result's copy of the report: the tool_result is
    // truncated for DB storage past maxToolResultDBSize, so on a long report it
    // is the same text with its middle cut out. The roster promises the whole
    // report, so it reads the copy that still is one. A backgrounded run has
    // neither — its agent_result is the empty launch marker and its tool_result
    // is the launch receipt — so its report is the terminal notification's
    // summary, which is where the CLI delivers it.
    const report =
      agentReportByToolUse.get(toolUseId) ?? (asyncLaunched ? summary : resultText(result));
    const flatTitle = flattenPreview(title);
    const preview = summary ? flattenPreview(summary) : report ? flattenPreview(report) : undefined;
    runs.push({
      toolUseId,
      title: flatTitle,
      agentType: asText(input.subagent_type),
      state,
      // A preview that repeats the title says nothing twice. Some task streams
      // set `summary` to the description verbatim, and a row that prints itself
      // over again reads as noise rather than as an outcome.
      preview: state === "running" || preview === flatTitle ? undefined : preview,
      report: state === "running" ? undefined : report,
      steps: stepsByToolUse.get(toolUseId) ?? [],
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

export interface AgentRunScope extends AgentRunPartition {
  /** Landed before the current turn — folded away by default. */
  earlier: AgentRun[];
}

/**
 * The roster as the dock shows it: still out, landed **this turn**, and
 * everything older held back.
 *
 * A lifetime roster is the same mistake `agentBadgeState` already refuses to
 * make. A list that only grows stops being read, and in a 300px column it is a
 * wall in front of the two agents you actually came for. Nothing is discarded —
 * `earlier` is one disclosure away, because an agent is readable, not just
 * reportable — but the default is the turn you are in.
 *
 * Runs still streaming have no `turnIndex` yet and belong to the latest turn,
 * which is exactly where the fallback puts them.
 */
export function scopeAgentRuns(runs: readonly AgentRun[], latestTurnIndex?: number): AgentRunScope {
  const { inFlight, landed } = partitionAgentRuns(runs);
  if (latestTurnIndex === undefined) return { inFlight, landed, earlier: [] };

  const current: AgentRun[] = [];
  const earlier: AgentRun[] = [];
  for (const run of landed) {
    if ((run.turnIndex ?? latestTurnIndex) >= latestTurnIndex) current.push(run);
    else earlier.push(run);
  }
  return { inFlight, landed: current, earlier };
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
  /** Agents still out. The only fact the badge raises. */
  running: number;
}

const NO_BADGE: AgentBadgeState = { running: 0 };

/**
 * What the Agents badge should say right now: how many agents are out, and
 * nothing else.
 *
 * A subagent that failed is deliberately *not* here. A badge is a claim on the
 * operator's attention, and a failed subagent is not the operator's to deal
 * with — the session that spawned it reads the failure and decides, usually by
 * trying again. Raising it said "this session needs you" about a turn that was
 * proceeding fine. The row still carries the outcome for anyone who opens Work;
 * what changed is that it no longer interrupts.
 *
 * Loops are the other way round and keep their failure mark: an auto-paused
 * schedule stays paused until a person acts (`loopBadgeState`).
 */
export function agentBadgeState(runs: readonly AgentRun[]): AgentBadgeState {
  if (runs.length === 0) return NO_BADGE;

  let running = 0;
  for (const run of runs) {
    if (run.state === "running") running += 1;
  }
  return { running };
}

export interface AgentRunTotals {
  running: number;
  done: number;
  failed: number;
  stopped: number;
  totalTokens: number;
}

export function agentRunTotals(runs: readonly AgentRun[]): AgentRunTotals {
  const totals: AgentRunTotals = { running: 0, done: 0, failed: 0, stopped: 0, totalTokens: 0 };
  for (const run of runs) {
    totals[run.state] += 1;
    totals.totalTokens += run.totalTokens;
  }
  return totals;
}
