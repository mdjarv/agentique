/**
 * Pure derivation helpers for the thread sidebar — no React, no stores.
 * The integrator feeds primitives pulled from the chat store; these decide
 * badge, machine-line phrasing, and Open-section ordering.
 */
import type { MachineLine, ThreadBadge, ThreadRowVM, WorkKind } from "./types";

/** The run reached an end — the CLI is not going to produce anything more. */
export function isTerminalState(state: string): boolean {
  return state === "done" || state === "stopped" || state === "failed";
}

/**
 * Pulse tool category → work kind. The categories come from the pulse pipeline
 * (`PulseStatus`'s `CATEGORY_LABELS` names the same set); anything unmapped —
 * including a session that hasn't reported a tool yet — falls to `generic`, so
 * a new upstream category degrades to the plain working glyph rather than
 * blanking the marker.
 */
const WORK_KIND_BY_CATEGORY: Record<string, WorkKind> = {
  command: "run",
  file_write: "edit",
  file_read: "read",
  web: "web",
  agent: "delegate",
  task: "task",
  plan: "plan",
  meta: "configure",
  question: "tool",
  mcp: "tool",
  other: "generic",
};

/** What kind of work a running session is doing, from its pulse category. */
export function deriveWorkKind(category?: string): WorkKind {
  if (!category) return "generic";
  return WORK_KIND_BY_CATEGORY[category] ?? "generic";
}

export interface DeriveBadgeInput {
  state: string;
  hasPendingApproval: boolean;
  hasPendingQuestion: boolean;
  isPlanning: boolean;
  hasUnseenCompletion: boolean;
  connected: boolean;
}

/**
 * Map session primitives to the corner badge. Mirrors `resolveSessionState`
 * priorities: blocked-on-human first (the amber monopoly), then live work,
 * then outcomes; `off` only when an idle session lost its CLI (evicted /
 * disconnected) — a running session's live state always wins.
 *
 * The two blocked states share amber but not their glyph: an approval halts
 * the agent mid-tool, a question waits on an answer. Approval wins when both
 * are somehow pending — it is the one holding the process.
 */
export function deriveBadge(input: DeriveBadgeInput): ThreadBadge {
  if (input.hasPendingApproval) return "attention";
  if (input.hasPendingQuestion) return "question";
  if (input.state === "running") return input.isPlanning ? "planning" : "working";
  if (input.state === "merging") return "merging";
  if (input.state === "failed") return "failed";
  if (input.hasUnseenCompletion) return "unread";
  if (input.state === "idle" && !input.connected) return "off";
  return null;
}

/** A row is awake — and earns its third line — for every badge except rest
 *  ("off" = evicted counts as rest; its story is the rest token).
 *
 *  `unread` is deliberately NOT awake: a finished session isn't doing anything.
 *  Its signal is the NEW pill in the time slot, and dropping the third line
 *  makes the list *shorter* exactly when a batch of sessions lands.
 *
 *  Awake does NOT decide colour — see {@link isHued}. A stopped session is not
 *  awake and still carries its hue. */
export function isAwake(badge: ThreadBadge): boolean {
  return badge !== null && badge !== "off" && badge !== "unread";
}

export interface HuedInput {
  state: string;
  /** The user filed it away. */
  archived: boolean;
  /** The worktree landed on the base branch. */
  merged: boolean;
}

/**
 * Whether the row keeps its project hue.
 *
 * Colour tracks "is this still mine to deal with", never "is a CLI attached".
 * Losing the process is not an outcome: agentique reclaims idle CLIs, a restart
 * reaps every process group, and a crash takes one down — none of which say
 * anything about the work. All three end with a session that one message wakes
 * again, so greying it files the work away on the user's behalf, right next to
 * something merged last week.
 *
 * Grey is already the language of *filed*: the "Finished earlier" shelf and
 * Archived both render `compact`, which is grey and collapsed by construction.
 * Here it means the same thing and nothing else — the user archived it, or the
 * worktree landed and the run ended.
 */
export function isHued(input: HuedInput): boolean {
  if (input.archived) return false;
  return !(input.merged && isTerminalState(input.state));
}

/** Blocked on a human — the two amber states, sorted to the top of Open. */
export function isBlocked(badge: ThreadBadge): boolean {
  return badge === "attention" || badge === "question";
}

export interface DeriveLivePhraseInput {
  badge: ThreadBadge;
  /** Live narration while the agent works (e.g. "editing AppSidebar.tsx"). */
  liveStatus?: string;
  /** Short summary of the pending approval (e.g. "go test -race ./ws/..."). */
  approvalSummary?: string;
  /** The question being asked, when one is pending. */
  questionSummary?: string;
}

/**
 * The third line of an awake row: the state phrase in its tone. Null at rest.
 *
 * The row's glyph names the state, so this line never repeats it — it carries
 * only what the glyph cannot say (the file, the command, the question), and
 * falls back to one bare word when there is nothing specific to report.
 */
export function deriveLivePhrase(input: DeriveLivePhraseInput): MachineLine | null {
  switch (input.badge) {
    case "attention":
      return { text: input.approvalSummary || "needs you", tone: "attn" };
    case "question":
      return { text: input.questionSummary || "needs an answer", tone: "attn" };
    case "working":
      return { text: input.liveStatus || "working", tone: "work" };
    case "planning":
      return { text: input.liveStatus || "planning", tone: "work" };
    case "merging":
      return { text: input.liveStatus || "merging", tone: "merge" };
    case "failed":
      return { text: input.liveStatus || "failed", tone: "fail" };
    // unread has no phrase: the pill says it, and the outcome word ("done" /
    // "merged") rides the repo line like any other settled session's.
    default:
      return null;
  }
}

/** How long a terminal, seen session may rest in Open before the stale shelf collects it. */
export const STALE_AFTER_MS = 24 * 60 * 60 * 1000;

export interface StaleInput {
  state: string;
  /** Unseen completion — the operator hasn't looked yet, so it must stay visible. */
  unread: boolean;
  lastActivity: number;
  now: number;
}

/**
 * A session is stale when its run reached a terminal state, the operator has
 * seen the outcome, and nothing has happened for a day. Deliberately NOT
 * keyed on merge: an early merge mid-session is normal, and a merged session
 * that keeps working is never terminal anyway.
 */
export function isStale(input: StaleInput): boolean {
  if (!isTerminalState(input.state) || input.unread) return false;
  return input.now - input.lastActivity > STALE_AFTER_MS;
}

/**
 * Open-section comparator: sessions blocked on a human first, then
 * last-activity desc, then sessionId for a stable order.
 */
export function compareOpenRows(a: ThreadRowVM, b: ThreadRowVM): number {
  const aBlocked = isBlocked(a.badge) ? 0 : 1;
  const bBlocked = isBlocked(b.badge) ? 0 : 1;
  if (aBlocked !== bBlocked) return aBlocked - bBlocked;
  if (a.lastActivity !== b.lastActivity) return b.lastActivity - a.lastActivity;
  return a.sessionId.localeCompare(b.sessionId);
}

/** Which section of the sidebar a row belongs to. A row is in exactly one. */
export type ThreadSection = "pinned" | "open" | "stale" | "archived";

export interface SectionInput {
  /** The user filed it away. */
  archived: boolean;
  /** The user wants it at the top. */
  pinned: boolean;
  /** Terminal, seen, and quiet for a day — see {@link isStale}. */
  stale: boolean;
}

/**
 * Decide a row's section, precedence first.
 *
 * Archived outranks pinned, and that ordering is the whole point: "keep this at
 * the top" and "stow this away" are contradictory claims, so the newer gesture
 * wins and filed-away work never sits in the priority section. The server
 * releases the pin when it archives, but the view cannot wait for that — the
 * state push announcing an archive carries archivedAt and not pinned, and a peer
 * on an older release never clears the pin at all.
 */
export function sectionFor(input: SectionInput): ThreadSection {
  if (input.archived) return "archived";
  if (input.pinned) return "pinned";
  return input.stale ? "stale" : "open";
}
