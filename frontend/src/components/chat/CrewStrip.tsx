import { Link } from "@tanstack/react-router";
import { Check, ChevronDown, ChevronRight, Moon, TriangleAlert, X } from "lucide-react";
import { memo, useState } from "react";
import { useNow } from "~/hooks/useNow";
import { formatDuration } from "~/lib/format";
import { type Crew, type CrewMember, type CrewToken, crewLabel } from "~/lib/session/crew";
import { cn, sessionShortId } from "~/lib/utils";

/**
 * The crew a lead spawned, as chips above the composer.
 *
 * It rides the slot `AgentFlightStrip` already owns, and deliberately shares
 * its shape — but it is a different claim, and three things follow from that:
 *
 * - **It does not empty out.** A subagent vanishes when it returns; a worker
 *   reports and keeps going. So a chip carries state rather than presence, and
 *   the strip stays up for the whole run.
 * - **The label counts what is missing.** "2 out", not "In flight". A
 *   transcript can already show what came back; who has *not* is the question
 *   it structurally cannot answer, and the only reason this strip exists.
 * - **Chips navigate.** A subagent has no session to open and a worker does,
 *   which is why workers take the primary blue where agents keep their violet.
 *   One mark, one meaning: the clickable one is the one that looks clickable.
 *
 * It also cannot be gated on the lead being busy. Workers outlive the turn that
 * spawned them by design, so the moment the strip is most worth reading — the
 * lead idle, waiting on three check-ins — is exactly when a busy-gated strip
 * would be gone.
 */

/** Density, mirroring `AgentFlightStrip`: chips on desktop, one line on mobile. */
export type CrewDensity = "rail" | "line";

/**
 * One mark per state, closed with `CrewToken` so a new state must choose its
 * glyph. The vocabulary is the app's, not this component's: a triangle means
 * "someone is waiting on you" in `ThreadRow`, `DockToggle` and `DockTabBar`, an
 * X means "it failed", and a moon means the process is gone but the work is
 * not. `working` has no glyph on purpose — a pulsing dot is the app's mark for
 * live activity, and a picture there would compete with the three that matter.
 */
const CREW_GLYPH: Record<Exclude<CrewToken, "working">, typeof Check> = {
  waiting: TriangleAlert,
  failed: X,
  resting: Moon,
  back: Check,
};

/** What each state says on hover, and to a screen reader. */
const CREW_TITLE: Record<CrewToken, string> = {
  waiting: "Waiting on you",
  failed: "Failed",
  working: "Working",
  resting: "No process attached — the next message resumes it",
  back: "Reported back",
};

/**
 * Tint per state. Attention states take their own semantic colour rather than
 * the crew blue: a worker holding up the run has to read as urgent from the
 * corner of the eye, and blue is this strip's resting voice.
 */
const CREW_TINT: Record<CrewToken, string> = {
  waiting: "border-amber-500/40 bg-amber-500/10 text-foreground",
  failed: "border-destructive/40 bg-destructive/10 text-foreground",
  working: "border-primary/30 bg-primary/10 text-foreground",
  resting: "border-primary/20 bg-primary/[0.06] text-foreground-dim",
  back: "border-border/60 bg-muted/30 text-muted-foreground",
};

const MARK_TINT: Record<CrewToken, string> = {
  waiting: "text-amber-500",
  failed: "text-destructive",
  working: "bg-primary",
  resting: "text-muted-foreground-faint",
  back: "text-emerald-500",
};

interface CrewStripProps {
  crew: Crew;
  density: CrewDensity;
  /** Resolves a worker's project to the slug its route needs. */
  projectSlugFor: (projectId: string) => string | undefined;
  /** `line` only — lifted so expansion survives a navigation. */
  expanded?: boolean;
  onExpandedChange?: (expanded: boolean) => void;
  className?: string;
}

export function CrewStrip({
  crew,
  density,
  projectSlugFor,
  expanded,
  onExpandedChange,
  className,
}: CrewStripProps) {
  const [localExpanded, setLocalExpanded] = useState(false);
  // 30s rather than the flight strip's 1s: a worker's silence is a
  // minutes-scale reading, and a per-second tick here would re-render the
  // composer's neighbour once a second for the life of a run.
  const now = useNow(30_000, crew.members.length > 0).getTime();
  const isOpen = expanded ?? localExpanded;
  const setOpen = onExpandedChange ?? setLocalExpanded;

  if (crew.members.length === 0) return null;

  if (density === "line") {
    return (
      <div className={cn("shrink-0 border-t bg-primary/[0.05]", className)}>
        <button
          type="button"
          onClick={() => setOpen(!isOpen)}
          aria-expanded={isOpen}
          className="flex w-full cursor-pointer items-center gap-2 px-3 py-1.5 text-left text-xs"
        >
          <CrewPips members={crew.members} />
          <span className="text-foreground">
            {crewLabel(crew)}
            <span className="text-muted-foreground"> of {crew.members.length}</span>
          </span>
          {isOpen ? (
            <ChevronDown className="ml-auto size-3.5 text-muted-foreground" />
          ) : (
            <ChevronRight className="ml-auto size-3.5 text-muted-foreground" />
          )}
        </button>
        {isOpen && <ChipRail crew={crew} now={now} projectSlugFor={projectSlugFor} />}
      </div>
    );
  }

  return (
    <div className={cn("shrink-0 border-t bg-primary/[0.05]", className)}>
      <ChipRail crew={crew} now={now} projectSlugFor={projectSlugFor} label />
    </div>
  );
}

/**
 * Presence at mobile width, where three chip labels do not fit. Each pip takes
 * its state's colour, so the one fact that survives the squeeze is the one
 * worth keeping: whether anybody is blocked.
 */
function CrewPips({ members }: { members: CrewMember[] }) {
  const shown = members.slice(0, 5);
  return (
    <span className="flex items-center gap-[3px]">
      {shown.map((m) => (
        <span
          key={m.sessionId}
          className={cn(
            "size-1.5 rounded-full",
            m.token === "waiting" && "bg-amber-500",
            m.token === "failed" && "bg-destructive",
            m.token === "working" && "bg-primary motion-safe:animate-pulse",
            m.token === "resting" && "bg-muted-foreground-faint",
            m.token === "back" && "bg-emerald-500/70",
          )}
        />
      ))}
      {members.length > shown.length && (
        <span className="text-[10px] text-primary tabular-nums">
          +{members.length - shown.length}
        </span>
      )}
    </span>
  );
}

function ChipRail({
  crew,
  now,
  projectSlugFor,
  label = false,
}: {
  crew: Crew;
  now: number;
  projectSlugFor: (projectId: string) => string | undefined;
  label?: boolean;
}) {
  return (
    <div className="scrollbar-none flex items-center gap-1.5 overflow-x-auto px-3 py-1.5">
      {label && (
        <span className="shrink-0 font-medium text-[10px] text-primary/80 uppercase tracking-[0.1em] tabular-nums">
          {crewLabel(crew)}
        </span>
      )}
      {crew.members.map((member) => (
        <CrewChip
          key={member.sessionId}
          member={member}
          now={now}
          projectSlug={projectSlugFor(member.projectId)}
        />
      ))}
    </div>
  );
}

/**
 * A worker's clock reads the gap since it last did anything, not how long it
 * has run: silence is what a lead is judging. A worker that came back says so
 * in a word instead — a duration there invites the reader to do arithmetic on
 * a number that has stopped meaning anything.
 */
function chipClock(member: CrewMember, now: number): string | undefined {
  if (member.token === "back") return "done";
  if (member.lastActivityAt === undefined) return undefined;
  return formatDuration(Math.max(0, now - member.lastActivityAt));
}

const CrewChip = memo(function CrewChip({
  member,
  now,
  projectSlug,
}: {
  member: CrewMember;
  now: number;
  projectSlug: string | undefined;
}) {
  const name = member.name || "Unnamed worker";
  const clock = chipClock(member, now);
  const Glyph = member.token === "working" ? null : CREW_GLYPH[member.token];
  const title = `${name} — ${CREW_TITLE[member.token]}`;

  const inner = (
    <>
      {Glyph ? (
        <Glyph className={cn("size-3 shrink-0", MARK_TINT[member.token])} />
      ) : (
        <span
          className={cn(
            "size-1.5 shrink-0 rounded-full motion-safe:animate-pulse",
            MARK_TINT.working,
          )}
        />
      )}
      <span className="max-w-[11rem] truncate">{name}</span>
      {clock && <span className="text-[11px] text-muted-foreground tabular-nums">{clock}</span>}
    </>
  );

  const shape = cn(
    "flex shrink-0 items-center gap-1.5 rounded-full border py-0.5 pr-2.5 pl-2 text-xs",
    CREW_TINT[member.token],
  );

  // Without a slug there is no route to build — the worker's project has not
  // arrived yet, or lives on a machine this client cannot reach. Render the
  // chip as plain status rather than a link that goes nowhere.
  if (!projectSlug) {
    return (
      <span className={shape} title={title}>
        {inner}
      </span>
    );
  }

  return (
    <Link
      to="/project/$projectSlug/session/$sessionShortId"
      params={{ projectSlug, sessionShortId: sessionShortId(member.sessionId) }}
      title={title}
      aria-label={title}
      className={cn(shape, "transition-colors hover:brightness-125")}
    >
      {inner}
    </Link>
  );
});
