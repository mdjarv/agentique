import { ChevronDown, ChevronRight } from "lucide-react";
import { memo, useState } from "react";
import { ToolIcon } from "~/components/chat/ToolIcons";
import { useNow } from "~/hooks/useNow";
import { type AgentRun, flightElapsedMs, oldestFlightElapsedMs } from "~/lib/agent-runs";
import { formatDuration, formatTokens } from "~/lib/format";
import { cn } from "~/lib/utils";

/**
 * How much of an agent's flight status to show.
 *
 * - `rail` — chips, for the strip that follows you across tabs. A glance.
 * - `board` — cards with burn and tool counts, for the top of the Agents tab.
 *   A dwell.
 * - `line` — presence pips and the oldest agent's clock in one row, for
 *   mobile, where the composer is deliberately input-forward and a full rail
 *   costs too much height. Expands to `rail` in place.
 *
 * One component rather than two, because a rail and a board that disagree
 * about what an agent is doing are worse than either alone.
 */
export type FlightDensity = "rail" | "board" | "line";

interface AgentFlightStripProps {
  /** Runs still out. The strip renders nothing when empty. */
  inFlight: AgentRun[];
  density: FlightDensity;
  /** `line` only — lifted so expansion survives a tab switch. */
  expanded?: boolean;
  onExpandedChange?: (expanded: boolean) => void;
  /**
   * `board` only — what a card opens into. Given one, cards become buttons and
   * open in place; without it they stay plain status. The strip decides *that*
   * a card is open, never what "open" shows: reading an agent is the roster's
   * subject, and the rail and line stay a glance.
   */
  renderDetail?: (run: AgentRun) => React.ReactNode;
  className?: string;
}

export function AgentFlightStrip({
  inFlight,
  density,
  expanded,
  onExpandedChange,
  renderDetail,
  className,
}: AgentFlightStripProps) {
  const [localExpanded, setLocalExpanded] = useState(false);
  const [openCards, setOpenCards] = useState<ReadonlySet<string>>(() => new Set<string>());
  // The clock lives here rather than in the parent: a 1s tick in ChatPanel
  // would re-render the whole session view every second an agent is out.
  const now = useNow(1_000, inFlight.length > 0).getTime();
  const isOpen = expanded ?? localExpanded;
  const setOpen = onExpandedChange ?? setLocalExpanded;

  if (inFlight.length === 0) return null;

  if (density === "board") {
    const toggleCard = (toolUseId: string) =>
      setOpenCards((prev) => {
        const next = new Set(prev);
        if (!next.delete(toolUseId)) next.add(toolUseId);
        return next;
      });
    return (
      <div className={cn("shrink-0", className)}>
        <SectionHeading label={`In flight — ${inFlight.length}`} />
        {/* items-start so one opened card grows alone instead of stretching
            every card in its row to match. */}
        <div className="grid grid-cols-[repeat(auto-fit,minmax(13rem,1fr))] items-start gap-2 px-4 pb-3">
          {inFlight.map((run) => (
            <FlightCard
              key={run.toolUseId}
              run={run}
              now={now}
              detail={renderDetail?.(run)}
              open={openCards.has(run.toolUseId)}
              onToggle={renderDetail ? () => toggleCard(run.toolUseId) : undefined}
            />
          ))}
        </div>
      </div>
    );
  }

  if (density === "line") {
    const oldest = oldestFlightElapsedMs(inFlight, now);
    return (
      <div className={cn("shrink-0 border-t bg-agent/[0.06]", className)}>
        <button
          type="button"
          onClick={() => setOpen(!isOpen)}
          aria-expanded={isOpen}
          className="flex w-full items-center gap-2 px-3 py-1.5 text-left text-xs cursor-pointer"
        >
          <PresencePips count={inFlight.length} />
          <span className="text-foreground">{inFlight.length} in flight</span>
          {oldest !== undefined && (
            <span className="text-agent tabular-nums">{formatDuration(oldest)}</span>
          )}
          {isOpen ? (
            <ChevronDown className="ml-auto size-3.5 text-muted-foreground" />
          ) : (
            <ChevronRight className="ml-auto size-3.5 text-muted-foreground" />
          )}
        </button>
        {isOpen && <ChipRail inFlight={inFlight} now={now} />}
      </div>
    );
  }

  return (
    <div className={cn("shrink-0 border-t bg-agent/[0.06]", className)}>
      <ChipRail inFlight={inFlight} now={now} label />
    </div>
  );
}

function SectionHeading({ label }: { label: string }) {
  return (
    <div className="flex items-center gap-2 px-4 pt-2 pb-1.5">
      <span className="font-medium text-[10px] text-muted-foreground-faint uppercase tracking-[0.12em]">
        {label}
      </span>
      <span className="h-px flex-1 bg-border/60" />
    </div>
  );
}

/**
 * Presence rather than a numeral: at mobile sizes three dots fit where the
 * count plus three chip labels do not, and concurrency is a quantity you sense
 * rather than one you do arithmetic on.
 */
function PresencePips({ count }: { count: number }) {
  const shown = Math.min(count, 5);
  return (
    <span className="flex items-center gap-[3px]">
      {Array.from({ length: shown }, (_, i) => (
        <span
          // biome-ignore lint/suspicious/noArrayIndexKey: pips are positional, not identities
          key={i}
          className="size-1.5 rounded-full bg-agent motion-safe:animate-pulse"
          style={{ animationDelay: `${i * 250}ms` }}
        />
      ))}
      {count > shown && (
        <span className="text-[10px] text-agent tabular-nums">+{count - shown}</span>
      )}
    </span>
  );
}

function ChipRail({
  inFlight,
  now,
  label = false,
}: {
  inFlight: AgentRun[];
  now: number;
  label?: boolean;
}) {
  return (
    <div className="scrollbar-none flex items-center gap-1.5 overflow-x-auto px-3 py-1.5">
      {label && (
        <span className="shrink-0 font-medium text-[10px] text-agent/80 uppercase tracking-[0.1em]">
          In flight
        </span>
      )}
      {inFlight.map((run) => (
        <FlightChip key={run.toolUseId} run={run} now={now} />
      ))}
    </div>
  );
}

const FlightChip = memo(function FlightChip({ run, now }: { run: AgentRun; now: number }) {
  const elapsed = flightElapsedMs(run, now);
  return (
    <span
      title={run.lastToolName ? `${run.title} — ${run.lastToolName}` : run.title}
      className="flex shrink-0 items-center gap-1.5 rounded-full border border-agent/30 bg-agent/10 py-0.5 pr-2.5 pl-2 text-xs"
    >
      <span className="size-1.5 rounded-full bg-agent motion-safe:animate-pulse" />
      <span className="max-w-[11rem] truncate text-foreground">{run.title}</span>
      {elapsed !== undefined && (
        <span className="text-[11px] text-agent tabular-nums">{formatDuration(elapsed)}</span>
      )}
    </span>
  );
});

const FlightCard = memo(function FlightCard({
  run,
  now,
  detail,
  open = false,
  onToggle,
}: {
  run: AgentRun;
  now: number;
  detail?: React.ReactNode;
  open?: boolean;
  onToggle?: () => void;
}) {
  const elapsed = flightElapsedMs(run, now);
  const metrics = [
    run.totalTokens > 0 ? `${formatTokens(run.totalTokens)} tok` : null,
    run.toolUses > 0 ? `${run.toolUses} ${run.toolUses === 1 ? "tool" : "tools"}` : null,
  ].filter((part): part is string => part !== null);

  const body = (
    <>
      <div className="flex items-center gap-2">
        <span className="size-1.5 shrink-0 rounded-full bg-agent motion-safe:animate-pulse" />
        <span className="truncate text-foreground text-xs">{run.title}</span>
        {elapsed !== undefined && (
          <span className="ml-auto shrink-0 text-[11px] text-agent tabular-nums">
            {formatDuration(elapsed)}
          </span>
        )}
      </div>
      {run.lastToolName && (
        <span className="mt-1.5 flex items-center gap-1.5 text-[11px] text-muted-foreground-faint">
          <ToolIcon name={run.lastToolName} />
          <span className="truncate">{run.lastToolName}</span>
        </span>
      )}
      {metrics.length > 0 && (
        <p className="mt-1 text-[11px] text-muted-foreground-faint tabular-nums">
          {metrics.join(" · ")}
        </p>
      )}
    </>
  );

  const shell = "rounded-md border border-agent/25 bg-agent/[0.06] px-2.5 py-2";

  if (!onToggle) return <div className={shell}>{body}</div>;

  return (
    <div className={shell}>
      <button
        type="button"
        onClick={onToggle}
        aria-expanded={open}
        className="block w-full cursor-pointer text-left"
      >
        {body}
        <span className="mt-1.5 flex items-center gap-1 text-[11px] text-agent/80">
          {open ? (
            <ChevronDown className="size-3 shrink-0" />
          ) : (
            <ChevronRight className="size-3 shrink-0" />
          )}
          {open ? "Hide" : "Watch"}
        </span>
      </button>
      {open && <div className="mt-1.5 border-t pt-1.5">{detail}</div>}
    </div>
  );
});
