import type { SessionStorage } from "~/lib/generated-types";

/**
 * The rules a Storage selection obeys, kept out of the component so what the
 * bar is allowed to do is one testable function rather than a chain of
 * conditions spread across JSX.
 *
 * Two verbs, two bars:
 *
 * - **Reclaim** removes the checked-out tree, the browser profile and the
 *   scratchpad, keeping the session row and its git branch. Resuming
 *   re-provisions from the branch, so this is reversible and needs only that
 *   the session is finished and has no uncommitted work.
 * - **Delete** removes the row, the branch and the tree. Irreversible, so it
 *   requires the server to have established that the branch's commits already
 *   exist on the project's main line.
 *
 * Both verdicts come from the server, which is the only side that can ask git.
 */

/** The server's verdict on deleting a session. Absent = not established. */
export const SAFE = "safe";

/**
 * Whether the reversible verb is offered for this session.
 *
 * A peer that predates the field sends neither `reclaimable` nor `safety`; the
 * absent boolean arrives as `undefined`, which is correctly falsy. Nothing is
 * offered rather than something being wrongly offered.
 */
export function canReclaim(s: SessionStorage): boolean {
  return s.reclaimable === true;
}

/**
 * Whether this session clears the bulk-delete bar.
 *
 * Deliberately three-valued in spirit: `safety === "safe"` is a positive claim
 * the server made, and anything else — blocked, or an older peer that never
 * spoke the field — is "not established", never "safe". Reading a missing
 * field as permission is how a bulk destructive action ends up acting on work
 * nobody checked.
 */
export function canDelete(s: SessionStorage): boolean {
  return s.safety === SAFE;
}

/** What a session's row and the bar report as freed if it is reclaimed. */
export function freedBytes(s: SessionStorage): number {
  return s.totalBytes || s.bytes;
}

export interface SelectionSummary {
  /** Sessions in the selection. */
  count: number;
  /** Disk the whole selection accounts for. */
  bytes: number;
  /** The subset the reversible verb applies to. */
  reclaimable: SessionStorage[];
  /** The subset that clears the delete bar. */
  deletable: SessionStorage[];
  /**
   * Why Delete is unavailable for this selection, in the user's terms. Empty
   * when every selected session clears the bar.
   *
   * A count, not a list: naming one blocker in a bar makes the other four
   * invisible, and the rows themselves already carry the reason.
   */
  deleteBlockedReason: string;
  /** As above, for Reclaim. */
  reclaimBlockedReason: string;
}

/**
 * Summarize a selection.
 *
 * Blocked-ness is reported per selection rather than per row because that is
 * what the bar acts on: a Delete that silently skipped the three unsafe rows
 * would be a different action than the one the button offered.
 */
export function summarize(selected: SessionStorage[]): SelectionSummary {
  const reclaimable = selected.filter(canReclaim);
  const deletable = selected.filter(canDelete);
  return {
    count: selected.length,
    bytes: selected.reduce((a, s) => a + freedBytes(s), 0),
    reclaimable,
    deletable,
    deleteBlockedReason: blockedReason(selected.length, deletable.length),
    reclaimBlockedReason: blockedReason(selected.length, reclaimable.length),
  };
}

function blockedReason(total: number, allowed: number): string {
  if (total === 0 || allowed === total) return "";
  if (allowed === 0) {
    return total === 1 ? "This session is not eligible" : "None of these are eligible";
  }
  return `${total - allowed} of ${total} are not eligible`;
}

/**
 * Drop ids that are no longer on the page.
 *
 * The usage walk is cached for a minute and refreshed after every action, so a
 * selection routinely outlives the rows it was made from. Keeping a vanished id
 * would let a later click act on a set the user can no longer see.
 */
export function reconcile(selected: Set<string>, present: SessionStorage[]): Set<string> {
  const alive = new Set(present.map((s) => s.sessionId));
  const next = new Set<string>();
  for (const id of selected) {
    if (alive.has(id)) next.add(id);
  }
  return next;
}
