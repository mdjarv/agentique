/**
 * The usage detail, opened from the footer (docs/usage.md).
 *
 * One section per agent: name and plan tier, any status text explaining stale
 * or missing data, then one row per window — label, countdown, percentage, and
 * a full-width track beneath it. Then a line of local stats.
 *
 * The panel and the indicator must always agree about which windows exist, so
 * both read `usableLimits`. Neither draws a window whose percent is unknown.
 *
 * The disk section is a LINK to /storage, on the same argument that makes the
 * footer's compact gauge one: a level is a reading of a page, so the meter is
 * the way to it. Every disk meter this app draws therefore leads to the same
 * place — a section that looks exactly like the gauge beside it but answers
 * nothing when tapped is the worse half of "a destination gets one home",
 * especially on touch where the compact gauge is a 24px target.
 *
 * An allowance stays inert, because there is nowhere for it to go: the meter
 * and its reset are the whole of what there is to know.
 */
import { Link } from "@tanstack/react-router";
import { ChevronRight, RefreshCw } from "lucide-react";
import { useEffect, useState } from "react";
import { agentColor, ProviderMark } from "~/components/usage/ProviderMark";
import type { UsageAgent } from "~/lib/generated-types";
import {
  compactTokens,
  countdown,
  drawFraction,
  isGauge,
  limitTier,
  renderableAgents,
  STORAGE_AGENT_ID,
  usableLimits,
} from "~/lib/usage-api";
import { cn } from "~/lib/utils";
import { useUsageStore } from "~/stores/usage-store";

/** Countdowns are minutes-scale. Ticking every 30s while the panel is open
 *  beats a per-second tick that repaints sixty times for a label that changes
 *  once — and it stops entirely when the panel closes. */
const TICK_MS = 30_000;

const TIER_TEXT: Record<string, string> = {
  warning: "text-warning",
  critical: "text-destructive",
};

export function UsagePanel({ onNavigate }: { onNavigate?: () => void }) {
  const doc = useUsageStore((s) => s.doc);
  const loading = useUsageStore((s) => s.loading);
  const fetch = useUsageStore((s) => s.fetch);
  const [now, setNow] = useState(() => Date.now());

  useEffect(() => {
    const id = setInterval(() => setNow(Date.now()), TICK_MS);
    return () => clearInterval(id);
  }, []);

  const agents = renderableAgents(doc);
  if (agents.length === 0) return null;

  return (
    <div className="flex flex-col">
      {agents.map((agent, i) => (
        <AgentSection
          key={agent.id}
          agent={agent}
          now={now}
          first={i === 0}
          onNavigate={onNavigate}
        />
      ))}
      <div className="mx-2 my-1 h-px bg-border/60" />
      <button
        type="button"
        onClick={() => void fetch(true)}
        disabled={loading}
        className="flex cursor-pointer items-center gap-2 rounded-md px-3 py-2 text-xs text-foreground transition-colors hover:bg-muted/50 disabled:opacity-60"
      >
        <RefreshCw
          className={cn(
            "size-3.5 text-muted-foreground",
            loading && "animate-spin motion-reduce:animate-none",
          )}
        />
        Refresh now
      </button>
    </div>
  );
}

function AgentSection({
  agent,
  now,
  first,
  onNavigate,
}: {
  agent: UsageAgent;
  now: number;
  first: boolean;
  onNavigate?: () => void;
}) {
  const limits = usableLimits(agent);
  const gauge = isGauge(agent);
  const identity = agentColor(agent.id);
  const leadsToStorage = agent.id === STORAGE_AGENT_ID;

  // One body, two wrappers: the disk section is a link and every allowance is a
  // plain block, so nothing inert wears a link's affordances. The body is built
  // once and handed to whichever wraps it — a wrapper *component* declared here
  // would be a new type every render and remount the meters under it.
  const shellClass = cn(
    "flex flex-col gap-0.5",
    !first && "mt-1 border-t border-border/40 pt-1.5",
    leadsToStorage && "rounded-md transition-colors hover:bg-muted/50",
  );

  const body = (
    <>
      <div className="flex items-baseline gap-2 px-3 pb-0.5 pt-1">
        <ProviderMark id={agent.id} className="size-3 shrink-0 self-center" />
        <span className="truncate text-[11.5px] font-medium text-foreground-bright">
          {agent.name}
        </span>
        {agent.tierLabel && (
          <span className="ml-auto shrink-0 font-mono text-[10px] text-muted-foreground-faint">
            {agent.tierLabel}
          </span>
        )}
        {/* Says the section leads somewhere. Without it the only difference
            between the disk meter and the allowances above it is that one
            responds to a tap, which is not a difference you can see. */}
        {leadsToStorage && !agent.tierLabel && (
          <ChevronRight className="ml-auto size-3 shrink-0 self-center text-muted-foreground-faint" />
        )}
      </div>

      {/* A failure explains itself and never blanks the numbers above it. */}
      {agent.usageStatusText && (
        <div className="px-3 pb-1 text-[10px] leading-snug text-warning">
          {agent.usageStatusText}
          {agent.authHelpText ? (
            <span className="block text-muted-foreground-faint">{agent.authHelpText}</span>
          ) : null}
        </div>
      )}

      {limits.map((limit) => {
        const tier = limitTier(limit, gauge);
        // A gauge has nothing to reset to, so it shows its absolute figure
        // where an allowance shows its countdown.
        const right = gauge ? limit.detail : countdown(limit.resetsAt, now);
        return (
          <div key={limit.label} className="flex flex-col gap-1 px-3 py-1">
            <div className="flex items-baseline gap-2">
              <span className="min-w-0 flex-1 truncate text-[11px] text-foreground">
                {limit.label}
              </span>
              {right && (
                <span className="shrink-0 font-mono text-[9.5px] tabular-nums text-muted-foreground-faint">
                  {right}
                </span>
              )}
              <span
                className={cn(
                  "shrink-0 font-mono text-[10px] font-semibold tabular-nums",
                  TIER_TEXT[tier],
                )}
                style={TIER_TEXT[tier] ? undefined : { color: identity }}
              >
                {Math.round(limit.percent * 100)}%
              </span>
            </div>
            <span className="block h-[3px] overflow-hidden rounded-full bg-border/70">
              <span
                className="block h-full rounded-full"
                style={{
                  width: `${Math.round(drawFraction(limit.percent) * 100)}%`,
                  background:
                    tier === "critical"
                      ? "var(--destructive)"
                      : tier === "warning"
                        ? "var(--warning)"
                        : identity,
                }}
              />
            </span>
          </div>
        );
      })}

      {(agent.todayTokens || agent.todayPrompts) && (
        <div
          className="px-3 pb-1 pt-0.5 font-mono text-[9.5px] text-muted-foreground-faint"
          title="Spent through agentique today — work done outside it is not counted."
        >
          today {compactTokens(agent.todayTokens ?? 0)} · {agent.todayPrompts ?? 0} prompts
        </div>
      )}
    </>
  );

  if (!leadsToStorage) return <div className={shellClass}>{body}</div>;
  return (
    <Link to="/storage" onClick={onNavigate} className={shellClass} title="Open Storage">
      {body}
    </Link>
  );
}
