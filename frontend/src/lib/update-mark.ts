/**
 * What a fleet is waiting for, as one closed union with one glyph each
 * (docs/upgrades.md).
 *
 * The precedent is `lib/session/rest-state.ts`: both surfaces that report this
 * subject read ONE table, because a mark that says "upgrade" in the footer
 * cannot say something else in the popover it opens. The footer's `UpdateMark`
 * renders the winning kind; `UpdatePopoverRows` renders a row per waiting thing.
 * Neither decides what a kind looks like.
 *
 * The union is closed on purpose — a new kind has to choose its glyph and its
 * words here rather than inherit a blank mark.
 */

import { CircleArrowUp, type LucideIcon, RotateCw } from "lucide-react";
import type { UpdateStatus } from "~/lib/generated-types";
import { sourceVerdict } from "~/lib/update-source";

export type MarkKind =
  /** A published release to download. */
  | "release"
  /** A local checkout that has moved: compile it. */
  | "rebuild"
  /** A newer binary already installed; only the process is old. */
  | "restart"
  /** Several machines, with no single thing to name. */
  | "fleet";

/**
 * One glyph per kind, and deliberately not four different pictures.
 *
 * Downloading a release and compiling a checkout are the same offer to a reader
 * — there is a newer build, and taking it costs the current turn — so they wear
 * the same mark and the row's words say which. `CircleArrowUp` is a closed round
 * form, which is what makes it findable beside the usage cluster's field of
 * vertical strokes; an arrow drawn in strokes disappears into them.
 *
 * A restart is the one that is genuinely a different act: nothing to fetch,
 * nothing to compile, just bounce the process. Same split `sourceVerdict` makes
 * when it ranks `staged` above everything else.
 */
export const MARK_GLYPH: Record<MarkKind, LucideIcon> = {
  release: CircleArrowUp,
  rebuild: CircleArrowUp,
  restart: RotateCw,
  fleet: CircleArrowUp,
};

export interface Waiting {
  kind: MarkKind;
  /** The one line, for the accessible name of the control the mark rides. */
  label: string;
}

/**
 * What is waiting across these machines, or null when nothing is.
 *
 * Two claims can light the mark — a published release, or a local checkout that
 * has moved — and only one of them has a version to name. Several machines have
 * no single thing to name at all, so they say how many.
 */
export function waitingFor(
  behind: string[],
  statuses: Record<string, UpdateStatus>,
): Waiting | null {
  if (behind.length === 0) return null;
  if (behind.length !== 1) {
    return { kind: "fleet", label: `${behind.length} machines behind` };
  }

  const status = statuses[behind[0] as string];
  if (status?.behind) {
    return { kind: "release", label: `Update ${status.latest || "available"}` };
  }

  const verdict = sourceVerdict(status?.source);
  if (verdict.token === "staged") return { kind: "restart", label: "Restart to finish" };
  if (verdict.token === "ready") return { kind: "rebuild", label: "Rebuild available" };
  return { kind: "release", label: "Update available" };
}
