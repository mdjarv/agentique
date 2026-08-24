import type { ScheduleInfo } from "~/lib/generated-types";

/**
 * What a session's loops need from the user.
 *
 * - `blocked` — someone is waiting on you. Either the schedule itself is
 *   parked for approval, or a run is (`action_needed` is set when a run is
 *   "waiting on <pending>", went overdue, or ended needing a look). The badge
 *   does not separate the two: both mean "open Loops", and the panel says
 *   which. Clears when the server clears it — viewing the tab is enough.
 * - `paused` — the loop auto-paused on repeated failures. This one **survives
 *   being looked at**: only an explicit act (edit, or re-enable) clears it,
 *   which is the scheduler's rule, not a UI choice.
 *
 * `blocked` outranks `paused` because that is how the app ranks states
 * everywhere else — see `lib/session/priority.ts`, where approval and question
 * sit above failure. A stopped loop is bad; a loop that is waiting on a human
 * is the one only you can unstick.
 */
export type LoopAttentionKind = "blocked" | "paused";

export interface LoopBadgeState {
  kind: LoopAttentionKind;
  /** How many of this session's loops are in that state. */
  count: number;
}

function kindOf(schedule: ScheduleInfo): LoopAttentionKind | null {
  if (schedule.pauseReason === "pending-approval") return "blocked";
  if (schedule.attention === "action_needed") return "blocked";
  if (schedule.attention === "failed") return "paused";
  return null;
}

/**
 * What the Loops tab badge should say. `null` when nothing needs the user —
 * the tab still shows, because the loops still exist, but it stops asking.
 *
 * Only the more urgent kind is reported: a badge that tries to say two things
 * at once says neither.
 */
export function loopBadgeState(schedules: readonly ScheduleInfo[]): LoopBadgeState | null {
  let blocked = 0;
  let paused = 0;
  for (const schedule of schedules) {
    const kind = kindOf(schedule);
    if (kind === "blocked") blocked += 1;
    else if (kind === "paused") paused += 1;
  }
  if (blocked > 0) return { kind: "blocked", count: blocked };
  if (paused > 0) return { kind: "paused", count: paused };
  return null;
}
