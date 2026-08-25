/**
 * One agent, opened: what it came back with, and — behind a fold — how it got
 * there.
 *
 * The report leads because it is what the reader came for, and it renders as
 * markdown through the same component the chat uses, so an agent's headings and
 * code blocks read the same wherever you meet them. Narration is a second
 * question ("what did it actually do"), so it folds away behind the report: it
 * is long by nature and would push the report off the panel every time.
 *
 * Both halves are already on the wire, so opening a row costs a render, not a
 * fetch.
 */
import { ChevronDown, ChevronRight } from "lucide-react";
import { useMemo, useState } from "react";
import { Markdown } from "~/components/chat/Markdown";
import { SubagentSteps, subagentSteps } from "~/components/chat/SubagentSteps";
import type { AgentRun } from "~/lib/agent-runs";

/** "Show 12 steps so far" — an agent still out has not finished counting. */
function stepsLabel(count: number, running: boolean, open: boolean): string {
  const noun = count === 1 ? "step" : "steps";
  return `${open ? "Hide" : "Show"} ${count} ${noun}${running ? " so far" : ""}`;
}

export function AgentRunDetail({ run }: { run: AgentRun }) {
  const steps = useMemo(() => subagentSteps(run.steps), [run.steps]);
  // Collapsed when there is a report to read, open when the narration is all
  // there is — an agent still out has nothing else to show, and "Watch" that
  // opens onto a second button to press is not watching.
  const [showSteps, setShowSteps] = useState(() => !run.report);

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
    <div className="space-y-2 text-xs">
      {run.report && (
        // Selectable and whole: the report is the thing worth copying out.
        <div className="max-h-96 overflow-y-auto rounded border bg-muted/20 px-2.5 py-1.5">
          <Markdown content={run.report} />
        </div>
      )}

      {steps.length > 0 && (
        <div className="overflow-hidden rounded border bg-muted/20">
          <button
            type="button"
            onClick={() => setShowSteps((v) => !v)}
            aria-expanded={showSteps}
            className="flex w-full cursor-pointer items-center gap-1.5 px-2 py-1.5 text-muted-foreground-faint transition-colors hover:bg-muted/30 hover:text-muted-foreground"
          >
            {showSteps ? (
              <ChevronDown className="size-3 shrink-0" />
            ) : (
              <ChevronRight className="size-3 shrink-0" />
            )}
            <span>{stepsLabel(steps.length, run.state === "running", showSteps)}</span>
          </button>
          {showSteps && (
            <div className="max-h-64 overflow-y-auto border-t px-2 py-1.5">
              <SubagentSteps steps={steps} />
            </div>
          )}
        </div>
      )}
    </div>
  );
}
