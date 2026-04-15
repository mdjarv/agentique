/**
 * Session state priority for aggregation.
 *
 * Given a set of sessions, determine the single "worst" (most urgent) state
 * to represent the group. Used for collapsed project indicators, team badges, etc.
 *
 * Priority order (highest first):
 *   approval/question/plan → failed → running → unseen completion
 *
 * States at or below the VISIBILITY_THRESHOLD are not shown — they're
 * considered "normal" and don't warrant an indicator.
 */
import type { BadgeState } from "~/components/layout/session/SessionBadge";
import type { SessionData } from "~/stores/chat-store";

/** Priority values — lower = more urgent. */
const STATE_PRIORITY: Record<string, number> = {
  approval: 0,
  question: 0,
  plan: 0,
  failed: 1,
  running: 2,
  unseen: 3,
  // Everything below is "normal" — no indicator
  merging: 10,
  idle: 10,
  done: 10,
  stopped: 10,
};

/** States with priority > VISIBILITY_THRESHOLD produce no indicator. */
const VISIBILITY_THRESHOLD = 3;

/** Map a single session to its effective badge state. */
function sessionToBadgeState(data: SessionData): BadgeState {
  if (data.pendingApproval || data.pendingQuestion) {
    return data.planMode ? "plan" : "approval";
  }
  if (data.meta.state === "failed") return "failed";
  if (data.meta.state === "running") return "running";
  if (data.hasUnseenCompletion) return "unseen";
  return data.meta.state as BadgeState;
}

/**
 * Returns the most urgent BadgeState across a set of sessions,
 * or null if nothing is urgent enough to display.
 */
export function getWorstSessionState(sessions: Array<{ data: SessionData }>): BadgeState | null {
  let worst: BadgeState | null = null;
  let worstPri = Number.MAX_SAFE_INTEGER;

  for (const { data } of sessions) {
    const state = sessionToBadgeState(data);
    const pri = STATE_PRIORITY[state] ?? 10;
    if (pri < worstPri) {
      worstPri = pri;
      worst = state;
    }
  }

  if (worstPri > VISIBILITY_THRESHOLD) return null;
  return worst;
}
