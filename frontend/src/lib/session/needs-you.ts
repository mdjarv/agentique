/**
 * The three reasons a session is waiting on the operator, and the order they
 * are ranked in.
 *
 * One rule, one place: the landing deck's "Needs you" band and the voice call's
 * world snapshot both ask this question, and a session that says "waiting on
 * approval" on the deck cannot say "unread" to the agent on the phone.
 *
 * The two that hold a process come before the one that only holds the
 * operator's curiosity — the same ranking `lib/session/priority.ts` uses.
 */
import type { SessionData } from "~/stores/chat-types";

export type NeedsYouKind = "approval" | "question" | "unread";

/** Why this session is waiting on a human, or null when it is not. */
export function needsYou(data: SessionData): NeedsYouKind | null {
  if (data.pendingApproval) return "approval";
  if (data.pendingQuestion) return "question";
  // A running session's completion is the *previous* turn's — it is not
  // waiting to be read, it is being superseded.
  if (data.hasUnseenCompletion && data.meta.state !== "running") return "unread";
  return null;
}
