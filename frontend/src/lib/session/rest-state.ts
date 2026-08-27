/**
 * How a session that isn't running right now describes itself: one word and
 * one mark.
 *
 * Lives in `lib` because two surfaces read it — the thread sidebar's rows and
 * the landing deck's cards — and they must agree. A session that says
 * "evicted" in the rail cannot say "stopped" on the overview.
 */
import { Check, CloudOff, GitMerge, type LucideIcon, Moon, Unplug } from "lucide-react";

/**
 * The one-word outcome a resting row folds into its identity line. Closed,
 * because each token owns a glyph — a new one must pick its mark rather than
 * inherit a blank.
 */
export type RestToken = "" | "merged" | "stopped" | "finished" | "away" | "evicted";

/**
 * The mark beside the word. Deliberately quiet: the row keeps its project hue
 * either way, so this only has to separate *the work* ending (check, merge)
 * from *the process* ending (stop, unplug, cloud-off) — low-priority
 * information, since the next message wakes the session in every case.
 */
export const REST_GLYPH: Record<Exclude<RestToken, "">, LucideIcon> = {
  merged: GitMerge,
  finished: Check,
  // A moon, not a stop button: `CircleStop` is a control, and offering one on a
  // row where nothing is running invites a press that does nothing. What the
  // state means is asleep — the next message wakes it.
  stopped: Moon,
  evicted: Unplug,
  away: CloudOff,
};

/** A process is not attached, but the work is unfinished — one message resumes it. */
export function isParked(token: RestToken): boolean {
  return token === "stopped" || token === "evicted" || token === "away";
}

/**
 * The single mark every parked state wears in the sidebar, on the chip's
 * corner.
 *
 * One glyph for all three, where `REST_GLYPH` has three. That is not a
 * contradiction: the row shows *that* a session is parked, and the three
 * parked tokens differ only in why the process went away — which the next
 * message makes moot in every case. It is also forced by the size. The corner
 * mark is 9px, where `Unplug`'s six strokes and `CloudOff`'s slashed cloud
 * turn to mush; a moon is one closed curve and survives.
 *
 * Nothing is lost: the word is still in the row's aria-label and the chip's
 * tooltip, and the deck's cards — which have room — still print glyph and word
 * from `REST_GLYPH`.
 */
export const PARKED_GLYPH: LucideIcon = Moon;

/** What the chip's parked corner says on hover. */
export const PARKED_TITLE = "No process attached — the next message resumes it";

export interface DeriveRestTokenInput {
  state: string;
  merged: boolean;
  connected: boolean;
  /** That session's machine is unreachable — we are reading a cached row. */
  machineOffline?: boolean;
}

/**
 * The one-word outcome a resting row folds into its repo line.
 *
 * "finished" is the word for state `done`, never "done": the state means the CLI
 * exited cleanly, and "done" reads as the user's own verdict on the work. That
 * verdict is Archive now, and it lives in a section header rather than a token.
 *
 * "evicted" is a claim about what agentique DID — it reclaimed the CLI — so it
 * may only be said about a machine we can see. When the machine is away its
 * sessions are frozen to `connected: false` (see `markSessionsAway`) precisely
 * so no row claims to be live; reading that as "evicted" turns one honest
 * unknown into a false statement, since that CLI is most likely still running
 * over there. An unreachable machine gets "away", and the row's machine tag
 * says which one.
 */
export function deriveRestToken(input: DeriveRestTokenInput): RestToken {
  if (input.merged) return "merged";
  if (input.state === "stopped") return "stopped";
  if (input.state === "done") return "finished";
  if (input.machineOffline) return "away";
  if (!input.connected) return "evicted";
  return "";
}
