import { memo, useMemo } from "react";
import { ToolIcon } from "~/components/chat/ToolIcons";
import { type AgentRun, agentRunTotals } from "~/lib/agent-runs";
import { formatDuration, formatTokens } from "~/lib/format";
import { cn } from "~/lib/utils";

/**
 * Roster of every subagent this session spawned. The row's second line is the
 * head of what the agent *returned* — the whole point of the view is reading
 * four agents' conclusions without opening any of them.
 */
export function AgentsView({ runs }: { runs: AgentRun[] }) {
  const totals = useMemo(() => agentRunTotals(runs), [runs]);

  if (runs.length === 0) {
    return (
      <div className="flex-1 flex items-center justify-center px-6 text-center text-xs text-muted-foreground-faint">
        No agents spawned in this session yet.
      </div>
    );
  }

  return (
    <div className="flex-1 flex flex-col min-h-0">
      <div className="flex-1 overflow-y-auto divide-y divide-border/50">
        {runs.map((run) => (
          <AgentRunRow key={run.toolUseId} run={run} />
        ))}
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

      {run.state === "running" && run.lastToolName && (
        <span className="mt-0.5 flex items-center gap-1.5 pl-3.5 text-xs text-muted-foreground-faint">
          <ToolIcon name={run.lastToolName} />
          <span className="truncate">{run.lastToolName}</span>
        </span>
      )}

      {metrics.length > 0 && (
        <p className="mt-0.5 pl-3.5 text-[11px] text-muted-foreground-faint tabular-nums">
          {metrics.join(" · ")}
        </p>
      )}
    </div>
  );
});
