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

export interface DeriveMachineLineInput {
  state: string;
  badge: ThreadBadge;
  /** Live narration while the agent works (e.g. "editing AppSidebar.tsx"). */
  liveStatus?: string;
  /** Short summary of the pending approval (e.g. "go test -race ./ws/..."). */
  approvalSummary?: string;
  branch?: string;
  merged: boolean;
  completedAt?: string;
  remoteMachineLabel?: string;
}

/**
 * The mono second line: a live state phrase in the state hue when something
 * is happening, otherwise branch or outcome in faint. Remote sessions weave
 * the machine in as a prefix ("on <label> · <rest>") — never a separate glyph.
 */
export function deriveMachineLine(input: DeriveMachineLineInput): MachineLine {
  const body = machineLineBody(input);
  if (!input.remoteMachineLabel) return body;
  const text = body.text
    ? `on ${input.remoteMachineLabel} · ${body.text}`
    : `on ${input.remoteMachineLabel}`;
  return { text, tone: body.tone };
}

function machineLineBody(input: DeriveMachineLineInput): MachineLine {
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
    case "off":
      return { text: "resumes on next message", tone: "muted" };
    default:
      break;
  }
  // At rest: outcome outranks branch, branch outranks the archive fallback.
  if (input.merged) return { text: "merged", tone: "muted" };
  if (input.state === "stopped") return { text: "stopped by you", tone: "muted" };
  if (input.branch) return { text: input.branch, tone: "muted" };
  if (input.completedAt) return { text: "archived", tone: "muted" };
  return { text: "", tone: "muted" };
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
