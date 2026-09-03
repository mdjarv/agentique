/**
 * What the phone header's subline is about, right now.
 *
 * One line, four possible subjects, so the ranking *is* the design — and it
 * lives here rather than inside the component because it is the rule the
 * layout was chosen for, not a rendering detail.
 *
 * The order is the same one `lib/session/priority.ts` uses everywhere else:
 * what is happening beats what is scheduled, which beats what is merely
 * configured.
 *
 * 1. `work`  — live narration (`formatPulse`), or agents still out.
 * 2. `parked` — stopped with a loop that will wake it; "Stopped" reads as dead.
 * 3. `state` — a word worth reading: failed, waiting, merging, done.
 * 4. `brain` — idle. The state word here is "Idle", which the dot beside it
 *    already says, so the slot goes to which model answers and whether it
 *    stops to ask. That is what pays for the composer's tools row being gone:
 *    the line is free exactly when the reading is worth having.
 */
import type { BadgeState } from "~/components/layout/session/SessionBadge";

export type SublineSubject = "work" | "parked" | "state" | "brain";

export interface SublineInput {
  /** `hasLiveWork` — running, merging, or subagents still out. */
  live: boolean;
  /** Stopped, with an enabled schedule queued to fire. */
  parked: boolean;
  badgeState: BadgeState;
}

export function sublineSubject({ live, parked, badgeState }: SublineInput): SublineSubject {
  // Parked outranks live for one reason: a parked session is not running, so
  // `live` is false whenever `parked` is true. Checking it first keeps that
  // true by construction rather than by coincidence.
  if (parked) return "parked";
  if (live) return "work";
  return badgeState === "idle" ? "brain" : "state";
}
