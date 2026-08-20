import {
  AlertCircle,
  Check,
  ChevronDown,
  ChevronRight,
  CircleDashed,
  Loader2,
  Workflow as WorkflowIcon,
} from "lucide-react";
import { memo, useState } from "react";
import { ToolIcon } from "~/components/chat/ToolIcons";
import { formatTokens } from "~/lib/format";
import { cn } from "~/lib/utils";
import type { TaskEvent, WorkflowProgressEntry } from "~/stores/chat-types";

interface WorkflowActivityProps {
  taskEvents: TaskEvent[];
  /** When true, drop the inline chat indent (left rule) — for the right-panel view. */
  bare?: boolean;
}

function formatDuration(ms: number): string {
  if (ms <= 0) return "";
  if (ms >= 60_000) {
    const m = Math.floor(ms / 60_000);
    const s = Math.round((ms % 60_000) / 1000);
    return `${m}m${s ? ` ${s}s` : ""}`;
  }
  if (ms >= 1000) return `${(ms / 1000).toFixed(1)}s`;
  return `${ms}ms`;
}

type AgentState = WorkflowProgressEntry["state"];

function agentIsDone(s: AgentState): boolean {
  return s === "done" || s === "error";
}

function AgentStateIcon({ state }: { state: AgentState }) {
  if (state === "done") return <Check className="h-3 w-3 text-success shrink-0" />;
  if (state === "error") return <AlertCircle className="h-3 w-3 text-destructive shrink-0" />;
  if (state === "queued")
    return <CircleDashed className="h-3 w-3 text-muted-foreground-faint shrink-0" />;
  // start / progress
  return <Loader2 className="h-3 w-3 animate-spin text-agent/70 shrink-0" />;
}

const EMPTY_PROGRESS: WorkflowProgressEntry[] = [];

/**
 * Renders a dynamic workflow run (taskType === "local_workflow") as a phase →
 * agent tree, mirroring the CLI's /workflows view. Live state rides on the
 * latest task_progress event's workflowProgress[]; the run settles on
 * task_notification. Completed phases auto-collapse so the active phase stays in
 * focus during a long run; the whole panel collapses to its header when done.
 */
export const WorkflowActivity = memo(function WorkflowActivity({
  taskEvents,
  bare = false,
}: WorkflowActivityProps) {
  const started = taskEvents.find((e) => e.taskSubtype === "task_started");
  const latestProgress = taskEvents.findLast((e) => e.taskSubtype === "task_progress");
  const notification = taskEvents.find((e) => e.taskSubtype === "task_notification");

  // task_notification's status marks the terminal state ("completed" | "stopped"
  // | "killed" | ...). Absent ⇒ still running.
  const terminalStatus = notification?.taskStatus;
  const isDone = !!terminalStatus;
  const isError = terminalStatus === "stopped" || terminalStatus === "killed";

  const [bodyOpen, setBodyOpen] = useState(true);
  // Per-phase manual overrides; default is derived from completeness each render.
  const [phaseOverrides, setPhaseOverrides] = useState<Record<number, boolean>>({});

  if (!started) return null;

  const name =
    started.workflowName ||
    latestProgress?.workflowName ||
    notification?.workflowName ||
    started.taskDescription ||
    "workflow";

  const progress =
    latestProgress?.workflowProgress ?? notification?.workflowProgress ?? EMPTY_PROGRESS;
  const phases = progress
    .filter((p) => p.type === "workflow_phase")
    .sort((a, b) => a.index - b.index);
  const agents = progress.filter((p) => p.type === "workflow_agent");

  const totalAgents = agents.length;
  const doneAgents = agents.filter((a) => agentIsDone(a.state)).length;
  const erroredAgents = agents.filter((a) => a.state === "error").length;

  const latest = notification ?? latestProgress;
  const totalTokens = latest?.totalTokens ?? 0;
  const duration = latest?.durationMs ?? 0;

  const statusParts: string[] = [];
  if (totalAgents > 0) statusParts.push(`${doneAgents}/${totalAgents} agents`);
  if (totalTokens > 0) statusParts.push(`${formatTokens(totalTokens)} tok`);
  if (duration > 0) statusParts.push(formatDuration(duration));
  if (erroredAgents > 0) statusParts.push(`${erroredAgents} failed`);
  const statusLine = statusParts.join(" · ");

  const agentsByPhase = new Map<number, WorkflowProgressEntry[]>();
  for (const a of agents) {
    const key = a.phaseIndex ?? 0;
    const list = agentsByPhase.get(key);
    if (list) list.push(a);
    else agentsByPhase.set(key, [a]);
  }

  // No declared phases (e.g. a single-agent ultracode run) → render agents flat.
  const hasPhases = phases.length > 0;

  return (
    <div className={bare ? "" : "ml-5 border-l-2 border-agent/20 pl-2.5"}>
      <div className="border rounded-md bg-muted/20 overflow-hidden text-xs">
        {/* Header */}
        <button
          type="button"
          onClick={() => setBodyOpen((v) => !v)}
          className="flex w-full items-center gap-2 px-2 py-1.5 text-muted-foreground min-w-0 hover:bg-muted/30"
        >
          {bodyOpen ? (
            <ChevronDown className="h-3 w-3 shrink-0" />
          ) : (
            <ChevronRight className="h-3 w-3 shrink-0" />
          )}
          {isDone ? (
            isError ? (
              <AlertCircle className="h-3 w-3 text-destructive shrink-0" />
            ) : (
              <Check className="h-3 w-3 text-success shrink-0" />
            )
          ) : (
            <Loader2 className="h-3 w-3 animate-spin shrink-0" />
          )}
          <WorkflowIcon className="h-3 w-3 text-agent/70 shrink-0" />
          <span className="font-medium text-agent/80 shrink-0">{name}</span>
          {statusLine && (
            <span className="ml-auto text-muted-foreground-faint shrink-0 tabular-nums">
              {statusLine}
            </span>
          )}
        </button>

        {bodyOpen && (
          <div className="border-t">
            {hasPhases
              ? phases.map((phase) => {
                  const phaseAgents = agentsByPhase.get(phase.index) ?? [];
                  const phaseDone =
                    phaseAgents.length > 0 && phaseAgents.every((a) => agentIsDone(a.state));
                  const phaseRunning = phaseAgents.some(
                    (a) => a.state === "start" || a.state === "progress",
                  );
                  const open = phaseOverrides[phase.index] ?? !phaseDone;
                  const phaseDoneCount = phaseAgents.filter((a) => agentIsDone(a.state)).length;
                  return (
                    <div key={phase.index} className="border-b last:border-b-0">
                      <button
                        type="button"
                        onClick={() => setPhaseOverrides((o) => ({ ...o, [phase.index]: !open }))}
                        className="flex w-full items-center gap-2 px-2 py-1 text-muted-foreground min-w-0 hover:bg-muted/30"
                      >
                        {open ? (
                          <ChevronDown className="h-3 w-3 shrink-0 text-muted-foreground-faint" />
                        ) : (
                          <ChevronRight className="h-3 w-3 shrink-0 text-muted-foreground-faint" />
                        )}
                        {phaseDone ? (
                          <Check className="h-3 w-3 text-success shrink-0" />
                        ) : phaseRunning ? (
                          <Loader2 className="h-3 w-3 animate-spin text-agent/70 shrink-0" />
                        ) : (
                          <CircleDashed className="h-3 w-3 text-muted-foreground-faint shrink-0" />
                        )}
                        <span className="font-medium truncate">{phase.title || "Phase"}</span>
                        {phaseAgents.length > 0 && (
                          <span className="ml-auto text-muted-foreground-faint shrink-0 tabular-nums">
                            {phaseDoneCount}/{phaseAgents.length}
                          </span>
                        )}
                      </button>
                      {open && phaseAgents.length > 0 && (
                        <div className="pb-1">
                          {phaseAgents.map((a) => (
                            <AgentRow key={a.agentId || `${phase.index}-${a.index}`} agent={a} />
                          ))}
                        </div>
                      )}
                    </div>
                  );
                })
              : agents.map((a) => <AgentRow key={a.agentId || a.index} agent={a} />)}

            {notification?.taskSummary && (
              <div className="border-t px-2 py-1.5 text-muted-foreground-dim whitespace-pre-wrap">
                {notification.taskSummary}
              </div>
            )}
          </div>
        )}
      </div>
    </div>
  );
});

const AgentRow = memo(function AgentRow({ agent }: { agent: WorkflowProgressEntry }) {
  const meta: string[] = [];
  if (agent.tokens) meta.push(`${formatTokens(agent.tokens)} tok`);
  if (agent.durationMs) meta.push(formatDuration(agent.durationMs));
  const running = agent.state === "start" || agent.state === "progress";
  return (
    <div className="flex items-center gap-2 px-2 py-0.5 pl-7 text-muted-foreground-dim min-w-0">
      <AgentStateIcon state={agent.state} />
      <span className={cn("truncate", agent.state === "error" && "text-destructive")}>
        {agent.label || agent.agentId || "agent"}
      </span>
      {running && agent.lastToolName && (
        <span className="flex items-center gap-1 text-muted-foreground-faint min-w-0">
          <ToolIcon name={agent.lastToolName} />
          <span className="truncate">{agent.lastToolSummary || agent.lastToolName}</span>
        </span>
      )}
      {meta.length > 0 && (
        <span className="ml-auto text-muted-foreground-faint shrink-0 tabular-nums">
          {meta.join(" · ")}
        </span>
      )}
    </div>
  );
});
