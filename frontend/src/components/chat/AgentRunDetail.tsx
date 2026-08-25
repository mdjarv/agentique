/**
 * One agent, opened: what it has been doing, and what it came back with.
 *
 * The roster's rows answer "who is out and how did it go"; this answers the
 * question that used to mean leaving the tab — reading the agent itself. Both
 * halves are already on the wire, so opening a row costs a render, not a fetch.
 *
 * Narration first, report second, for the one reader who needs both: an agent
 * still out has only narration, and a landed one is usually read from its report
 * down. Each half scrolls on its own so a chatty agent cannot push the report
 * off the panel.
 */
import { useMemo } from "react";
import { SubagentSteps, subagentSteps } from "~/components/chat/SubagentSteps";
import type { AgentRun } from "~/lib/agent-runs";

function Heading({ label }: { label: string }) {
  return (
    <div className="mb-1 font-medium text-[10px] text-muted-foreground-faint uppercase tracking-[0.12em]">
      {label}
    </div>
  );
}

export function AgentRunDetail({ run }: { run: AgentRun }) {
  const steps = useMemo(() => subagentSteps(run.steps), [run.steps]);

  if (steps.length === 0 && !run.report) {
    return (
      <p className="text-[11px] text-muted-foreground-faint">
        {run.state === "running"
          ? "Nothing forwarded yet — this agent has not said anything the CLI passed on."
          : "No output forwarded. Reading an agent's own work needs a provider that forwards it (Claude, with [claude] forward-subagent-text)."}
      </p>
    );
  }

  return (
    <div className="space-y-2.5 text-xs">
      {steps.length > 0 && (
        <div>
          <Heading label={run.state === "running" ? "Doing" : "Did"} />
          <div className="max-h-64 overflow-y-auto rounded border bg-muted/20 px-2 py-1.5">
            <SubagentSteps steps={steps} />
          </div>
        </div>
      )}
      {run.report && (
        <div>
          <Heading label="Returned" />
          {/* Selectable and whole: the report is the thing worth copying out. */}
          <div className="max-h-80 overflow-y-auto whitespace-pre-wrap rounded border bg-muted/20 px-2 py-1.5 text-muted-foreground-dim">
            {run.report}
          </div>
        </div>
      )}
    </div>
  );
}
