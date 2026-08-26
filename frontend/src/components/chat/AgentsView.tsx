import { ChevronDown, ChevronRight } from "lucide-react";
import { memo, useCallback, useMemo, useState } from "react";
import { AgentFlightStrip } from "~/components/chat/AgentFlightStrip";
import { AgentRunDetail } from "~/components/chat/AgentRunDetail";
import { type AgentRun, agentRunTotals, scopeAgentRuns } from "~/lib/agent-runs";
import { formatDuration, formatTokens } from "~/lib/format";
import { cn } from "~/lib/utils";

/**
 * Roster of this session's subagents, in the only two groups a reader acts on:
 * still out, and came back. Landed rows are newest first — this is a roster,
 * not a transcript, and the reports you just asked for are the ones you came to
 * read.
 *
 * The row's second line is the head of what the agent *returned*: the whole
 * point of the view is reading four agents' conclusions without opening any of
 * them. Opening one goes the rest of the way — its narration and its report in
 * full — so the roster answers "what did it actually say" without sending the
 * reader back to the transcript. Lifetime totals live in the footer, which is
 * where an odometer belongs.
 *
 * Given `latestTurnIndex` it shows **this turn** and folds the rest behind one
 * disclosure (see `scopeAgentRuns`); without one it shows everything, which is
 * what a full-height surface should do.
 *
 * Which rows are open is view state and deliberately not persisted: a roster
 * that reopens yesterday's four agents is a wall, not a list.
 */
export function AgentsView({
  runs,
  latestTurnIndex,
  workflow,
}: {
  runs: AgentRun[];
  /** Scope the roster to this turn, folding older runs away. */
  latestTurnIndex?: number;
  /**
   * The session's live workflow tree, rendered under the flight board. A
   * workflow's agents are not `AgentRun`s — they ride the workflow's own
   * progress events — so this is a slot, not a merge.
   */
  workflow?: React.ReactNode;
}) {
  const totals = useMemo(() => agentRunTotals(runs), [runs]);
  const { inFlight, landed, earlier } = useMemo(
    () => scopeAgentRuns(runs, latestTurnIndex),
    [runs, latestTurnIndex],
  );
  const [openRows, setOpenRows] = useState<ReadonlySet<string>>(() => new Set<string>());
  const [earlierOpen, setEarlierOpen] = useState(false);

  const toggleRow = useCallback((toolUseId: string) => {
    setOpenRows((prev) => {
      const next = new Set(prev);
      if (!next.delete(toolUseId)) next.add(toolUseId);
      return next;
    });
  }, []);

  // The card and the row open into the same thing — one agent read one way.
  const renderDetail = useCallback((run: AgentRun) => <AgentRunDetail run={run} />, []);

  if (runs.length === 0) {
    return (
      <div className="flex-1 flex items-center justify-center px-6 text-center text-xs text-muted-foreground-faint">
        No agents spawned in this session yet.
      </div>
    );
  }

  return (
    <div className="flex-1 flex flex-col min-h-0">
      <AgentFlightStrip inFlight={inFlight} density="board" renderDetail={renderDetail} />
      {workflow && <div className="shrink-0 px-3 pb-2">{workflow}</div>}
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
            <AgentRunRow
              key={run.toolUseId}
              run={run}
              open={openRows.has(run.toolUseId)}
              onToggle={() => toggleRow(run.toolUseId)}
            />
          ))}
        </div>
        {earlier.length > 0 && (
          <>
            <button
              type="button"
              onClick={() => setEarlierOpen((v) => !v)}
              aria-expanded={earlierOpen}
              className="flex w-full cursor-pointer items-center gap-2 px-4 py-2 text-muted-foreground-faint transition-colors hover:text-foreground"
            >
              {earlierOpen ? (
                <ChevronDown className="size-3 shrink-0" />
              ) : (
                <ChevronRight className="size-3 shrink-0" />
              )}
              <span className="font-medium text-[10px] uppercase tracking-[0.12em]">
                Earlier · {earlier.length}
              </span>
              <span className="h-px flex-1 bg-border/60" />
            </button>
            {earlierOpen && (
              <div className="divide-y divide-border/50">
                {earlier.map((run) => (
                  <AgentRunRow
                    key={run.toolUseId}
                    run={run}
                    open={openRows.has(run.toolUseId)}
                    onToggle={() => toggleRow(run.toolUseId)}
                  />
                ))}
              </div>
            )}
          </>
        )}
      </div>
      <div className="shrink-0 flex items-center gap-3 border-t px-4 py-1.5 text-[11px] text-muted-foreground-faint">
        <span>
          {totals.done} done
          {totals.running > 0 && ` · ${totals.running} running`}
          {totals.stopped > 0 && ` · ${totals.stopped} stopped`}
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
  // Grey, because being shut down on purpose is not an incident.
  stopped: "bg-muted-foreground-faint",
};

const stateLabel: Record<AgentRun["state"], string> = {
  running: "Running",
  done: "Done",
  failed: "Failed",
  stopped: "Stopped",
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

/**
 * One landed run, closed or open. Agents still out are the flight strip's job,
 * not this row's.
 *
 * Closed, it is what it always was: outcome, title, the head of the report.
 * Open, it is the agent itself — so the whole row is the control, and the
 * one-line preview steps aside rather than repeating the first line of the
 * report printed directly under it.
 */
const AgentRunRow = memo(function AgentRunRow({
  run,
  open,
  onToggle,
}: {
  run: AgentRun;
  open: boolean;
  onToggle: () => void;
}) {
  // Metrics read as one sentence rather than a label/value grid — "91 tools"
  // needs no "Tools:" in front of it.
  const metrics = [
    run.totalTokens > 0 ? `${formatTokens(run.totalTokens)} tok` : null,
    run.toolUses > 0 ? `${run.toolUses} ${run.toolUses === 1 ? "tool" : "tools"}` : null,
  ].filter((part): part is string => part !== null);
  const Chevron = open ? ChevronDown : ChevronRight;

  return (
    <div className="px-4 py-2.5">
      <button
        type="button"
        onClick={onToggle}
        aria-expanded={open}
        aria-label={`${run.title} — ${open ? "hide" : "read"} this agent`}
        className="block w-full cursor-pointer text-left"
      >
        <span className="flex items-baseline gap-2 min-w-0">
          <StateDot state={run.state} />
          <span className="truncate text-sm text-foreground">{run.title}</span>
          {run.agentType && (
            <span className="shrink-0 rounded bg-agent/10 px-1.5 py-px font-medium text-[10px] text-agent/80">
              {run.agentType}
            </span>
          )}
          <span className="ml-auto flex shrink-0 items-baseline gap-1.5">
            {run.durationMs > 0 && (
              <span className="text-[11px] text-muted-foreground-faint tabular-nums">
                {formatDuration(run.durationMs)}
              </span>
            )}
            <Chevron className="size-3 self-center text-muted-foreground-faint" />
          </span>
        </span>

        {run.preview && !open && (
          <span
            className={cn(
              "mt-0.5 block truncate pl-3.5 text-xs",
              run.state === "failed" ? "text-destructive/80" : "text-muted-foreground-dim",
            )}
          >
            {run.preview}
          </span>
        )}

        {metrics.length > 0 && (
          <span className="mt-0.5 block pl-3.5 text-[11px] text-muted-foreground-faint tabular-nums">
            {metrics.join(" · ")}
          </span>
        )}
      </button>

      {open && (
        <div className="mt-2 pl-3.5">
          <AgentRunDetail run={run} />
        </div>
      )}
    </div>
  );
});
