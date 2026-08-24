import { memo, useMemo } from "react";
import { AgentFlightStrip } from "~/components/chat/AgentFlightStrip";
import { type AgentRun, agentRunTotals, partitionAgentRuns } from "~/lib/agent-runs";
import { formatDuration, formatTokens } from "~/lib/format";
import { cn } from "~/lib/utils";

/**
 * Roster of every subagent this session spawned, in the only two groups a
 * reader acts on: still out, and came back. Landed rows are newest first —
 * this is a roster, not a transcript, and the reports you just asked for are
 * the ones you came to read.
 *
 * The row's second line is the head of what the agent *returned*: the whole
 * point of the view is reading four agents' conclusions without opening any of
 * them. Lifetime totals live in the footer, which is where an odometer belongs.
 */
export function AgentsView({ runs }: { runs: AgentRun[] }) {
  const totals = useMemo(() => agentRunTotals(runs), [runs]);
  const { inFlight, landed } = useMemo(() => partitionAgentRuns(runs), [runs]);

  if (runs.length === 0) {
    return (
      <div className="flex-1 flex items-center justify-center px-6 text-center text-xs text-muted-foreground-faint">
        No agents spawned in this session yet.
      </div>
    );
  }

  return (
    <div className="flex-1 flex flex-col min-h-0">
      <AgentFlightStrip inFlight={inFlight} density="board" />
      <div className="flex-1 overflow-y-auto min-h-0">
        {inFlight.length > 0 && landed.length > 0 && (
          <div className="flex items-center gap-2 px-4 pt-2 pb-1.5">
            <span className="font-medium text-[10px] text-muted-foreground-faint uppercase tracking-[0.12em]">
              Landed
            </span>
            <span className="h-px flex-1 bg-border/60" />
          </div>
        )}
        <div className="divide-y divide-border/50">
          {landed.map((run) => (
            <AgentRunRow key={run.toolUseId} run={run} />
          ))}
        </div>
      </div>
      <div className="shrink-0 flex items-center gap-3 border-t px-4 py-1.5 text-[11px] text-muted-foreground-faint">
        <span>
          {totals.done} done
          {totals.running > 0 && ` · ${totals.running} running`}
          {totals.failed > 0 && ` · ${totals.failed} failed`}
        </span>
        {totals.totalTokens > 0 && (
          <span className="ml-auto tabular-nums">Σ {formatTokens(totals.totalTokens)} tok</span>
        )}
      </div>
    </div>
  );
}

const stateDotClass: Record<AgentRun["state"], string> = {
  running: "bg-agent animate-pulse",
  done: "bg-success",
  failed: "bg-destructive",
};

const stateLabel: Record<AgentRun["state"], string> = {
  running: "Running",
  done: "Done",
  failed: "Failed",
};

/**
 * State reads down the left edge rather than from a trailing icon: titles vary
 * in length, so a leading dot is the only marker that stays in one column and
 * lets a reader scan five agents' outcomes in one pass.
 */
function StateDot({ state }: { state: AgentRun["state"] }) {
  return (
    <span
      aria-label={stateLabel[state]}
      title={stateLabel[state]}
      className={cn("size-1.5 shrink-0 self-center rounded-full", stateDotClass[state])}
    />
  );
}

/** One landed run. Agents still out are the flight strip's job, not this row's. */
const AgentRunRow = memo(function AgentRunRow({ run }: { run: AgentRun }) {
  // Metrics read as one sentence rather than a label/value grid — "91 tools"
  // needs no "Tools:" in front of it.
  const metrics = [
    run.totalTokens > 0 ? `${formatTokens(run.totalTokens)} tok` : null,
    run.toolUses > 0 ? `${run.toolUses} ${run.toolUses === 1 ? "tool" : "tools"}` : null,
  ].filter((part): part is string => part !== null);

  return (
    <div className="px-4 py-2.5">
      <div className="flex items-baseline gap-2 min-w-0">
        <StateDot state={run.state} />
        <span className="truncate text-sm text-foreground">{run.title}</span>
        {run.agentType && (
          <span className="shrink-0 rounded bg-agent/10 px-1.5 py-px font-medium text-[10px] text-agent/80">
            {run.agentType}
          </span>
        )}
        {run.durationMs > 0 && (
          <span className="ml-auto shrink-0 text-[11px] text-muted-foreground-faint tabular-nums">
            {formatDuration(run.durationMs)}
          </span>
        )}
      </div>

      {run.preview && (
        <p
          className={cn(
            "mt-0.5 truncate pl-3.5 text-xs",
            run.state === "failed" ? "text-destructive/80" : "text-muted-foreground-dim",
          )}
        >
          {run.preview}
        </p>
      )}

      {metrics.length > 0 && (
        <p className="mt-0.5 pl-3.5 text-[11px] text-muted-foreground-faint tabular-nums">
          {metrics.join(" · ")}
        </p>
      )}
    </div>
  );
});
