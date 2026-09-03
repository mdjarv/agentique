/**
 * What the phone header's subline is about, right now.
 *
 * One line, three possible subjects, so the ranking *is* the design — and it
 * lives here rather than inside the component because it is the rule the
 * layout was chosen for, not a rendering detail.
 *
 * The order is the same one `lib/session/priority.ts` uses everywhere else:
 * what is happening beats what is scheduled, which beats a resting state.
 *
 * 1. `work`  — live narration (`formatPulse`), or agents still out.
 * 2. `parked` — stopped with a loop that will wake it; "Stopped" reads as dead.
 * 3. `state` — the resting word: idle, failed, waiting, merging, done.
 *
 * This decides the *left* of the line only. The right of it is the branch
 * cluster — where the code lives, how far ahead it is, and the one verb the
 * branch needs — which is fixed and does not compete for the slot.
 *
 * Note what is deliberately absent: the model and the permission mode. They
 * are settings, they live behind the composer's `+` tray, and a metadata line
 * that also carried them read as a control strip. See `CLAUDE.md`.
 */
export type SublineSubject = "work" | "parked" | "state";

export interface SublineInput {
  /** `hasLiveWork` — running, merging, or subagents still out. */
  live: boolean;
  /** Stopped, with an enabled schedule queued to fire. */
  parked: boolean;
}

export function sublineSubject({ live, parked }: SublineInput): SublineSubject {
  // Parked outranks live for one reason: a parked session is not running, so
  // `live` is false whenever `parked` is true. Checking it first keeps that
  // true by construction rather than by coincidence.
  if (parked) return "parked";
  if (live) return "work";
  return "state";
}
