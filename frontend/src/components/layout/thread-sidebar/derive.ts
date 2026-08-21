/**
 * Pure derivation helpers for the thread sidebar — no React, no stores.
 * The integrator feeds primitives pulled from the chat store; these decide
 * badge, machine-line phrasing, and Open-section ordering.
 */
import type { MachineLine, ThreadBadge, ThreadRowVM } from "./types";

export interface DeriveBadgeInput {
  state: string;
  hasPendingApproval: boolean;
  hasPendingQuestion: boolean;
  isPlanning: boolean;
  hasUnseenCompletion: boolean;
  connected: boolean;
  isDraft?: boolean;
}

/**
 * Map session primitives to the corner badge. Mirrors `resolveSessionState`
 * priorities: blocked-on-human first (the amber monopoly), then live work,
 * then outcomes; `off` only when an idle session lost its CLI (evicted /
 * disconnected) — a running session's live state always wins.
 */
export function deriveBadge(input: DeriveBadgeInput): ThreadBadge {
  if (input.hasPendingApproval || input.hasPendingQuestion) return "attention";
  if (input.isDraft) return "draft";
  if (input.state === "running") return input.isPlanning ? "planning" : "working";
  if (input.state === "merging") return "merging";
  if (input.state === "failed") return "failed";
  if (input.hasUnseenCompletion) return "unread";
  if (input.state === "idle" && !input.connected) return "off";
  return null;
}

/** A row is awake — and earns its color + third line — for every badge except
 *  rest ("off" = evicted counts as rest; its story is the rest token). */
export function isAwake(badge: ThreadBadge): boolean {
  return badge !== null && badge !== "off";
}

export interface DeriveLivePhraseInput {
  badge: ThreadBadge;
  /** Live narration while the agent works (e.g. "editing AppSidebar.tsx"). */
  liveStatus?: string;
  /** Short summary of the pending approval (e.g. "go test -race ./ws/..."). */
  approvalSummary?: string;
}

/** The third line of an awake row: the state phrase in its tone. Null at rest. */
export function deriveLivePhrase(input: DeriveLivePhraseInput): MachineLine | null {
  switch (input.badge) {
    case "attention":
      return input.approvalSummary
        ? { text: `approve · ${input.approvalSummary}`, tone: "attn" }
        : { text: "needs your input", tone: "attn" };
    case "working":
      return { text: input.liveStatus || "working…", tone: "work" };
    case "planning":
      return { text: input.liveStatus || "drafting a plan", tone: "work" };
    case "merging":
      return { text: input.liveStatus || "merging…", tone: "merge" };
    case "failed":
      return { text: input.liveStatus || "failed", tone: "fail" };
    case "unread":
      return { text: "finished — unread", tone: "unread" };
    case "draft":
      return { text: "draft — not sent", tone: "draft" };
    default:
      return null;
  }
}

export interface DeriveRestTokenInput {
  state: string;
  merged: boolean;
  connected: boolean;
}

/** The one-word outcome a resting row folds into its repo line. */
export function deriveRestToken(input: DeriveRestTokenInput): string {
  if (input.merged) return "merged";
  if (input.state === "stopped") return "stopped";
  if (input.state === "done") return "done";
  if (!input.connected) return "evicted";
  return "";
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
  const terminal = input.state === "done" || input.state === "stopped" || input.state === "failed";
  if (!terminal || input.unread) return false;
  return input.now - input.lastActivity > STALE_AFTER_MS;
}

/**
 * Open-section comparator: sessions blocked on a human first, then
 * last-activity desc, then sessionId for a stable order.
 */
export function compareOpenRows(a: ThreadRowVM, b: ThreadRowVM): number {
  const aBlocked = a.badge === "attention" ? 0 : 1;
  const bBlocked = b.badge === "attention" ? 0 : 1;
  if (aBlocked !== bBlocked) return aBlocked - bBlocked;
  if (a.lastActivity !== b.lastActivity) return b.lastActivity - a.lastActivity;
  return a.sessionId.localeCompare(b.sessionId);
}
