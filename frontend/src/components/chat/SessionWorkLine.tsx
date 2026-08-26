import { memo } from "react";
import { useShallow } from "zustand/react/shallow";
import { formatPulse } from "~/components/layout/session/PulseStatus";
import { cn } from "~/lib/utils";
import { usePulseStore } from "~/stores/pulse-store";

interface SessionWorkLineProps {
  sessionId: string;
  /** What the CLI process is doing — the run state, not the work. */
  state: string;
  /** Subagents still out, from `partitionAgentRuns`. */
  agentsInFlight: number;
  className?: string;
}

/**
 * What this session is doing right now, in the header of the session itself.
 *
 * The sidebar row has narrated live work since it was written (`livePhrase`),
 * and the chat's own header never did — so the surface you are *inside* said
 * less than the one you have to open, and a working session read as an idle
 * one. Both now say it, from the same rule (`formatPulse`).
 *
 * The agent clause is separate from the state word on purpose. A subagent
 * launched in the background outlives the turn that spawned it, so the run
 * state settles to idle while agents are still out — which is exactly the
 * moment the header looked most like nothing was happening. "Idle" stays true
 * about the process; the agents get their own claim.
 */
/**
 * Whether this session has live work to narrate — the one rule, so a caller
 * deciding what to show *instead* cannot drift from what the line renders.
 */
export function hasLiveWork(input: { state: string; agentsInFlight: number }): boolean {
  return input.state === "running" || input.state === "merging" || input.agentsInFlight > 0;
}

export const SessionWorkLine = memo(function SessionWorkLine({
  sessionId,
  state,
  agentsInFlight,
  className,
}: SessionWorkLineProps) {
  const pulse = usePulseStore(useShallow((s) => s.pulses[sessionId]));
  const working = state === "running" || state === "merging";
  const phrase = working ? (pulse ? formatPulse(pulse) : "") || "working" : "";

  if (!hasLiveWork({ state, agentsInFlight })) return null;

  return (
    <span
      className={cn("flex min-w-0 items-center gap-1.5", className)}
      title={phrase || undefined}
    >
      {phrase && <span className="truncate">{phrase}</span>}
      {agentsInFlight > 0 && (
        <span
          className="flex shrink-0 items-center gap-1 text-agent"
          title={`${agentsInFlight} ${agentsInFlight === 1 ? "agent is" : "agents are"} still out`}
        >
          <span className="size-1.5 rounded-full bg-agent motion-safe:animate-pulse" />
          <span className="tabular-nums">
            {agentsInFlight} {agentsInFlight === 1 ? "agent" : "agents"} out
          </span>
        </span>
      )}
    </span>
  );
});
