/**
 * The assistant's three WS ops.
 *
 * `assistant.say` is a mutation and runs on the socket's serial lane;
 * `assistant.history` and `assistant.journal` are reads and run on the
 * concurrent one. The say returns the ASK, not the answer — the head's reply
 * arrives as `assistant.delta` pushes and then one `assistant.message`, because
 * a blocking say would hold this connection's whole mutation lane for the
 * length of a turn.
 */

import {
  type AssistantJournalEntry,
  AssistantJournalEntrySchema,
  AssistantJournalResultSchema,
  type AssistantMessage,
  AssistantMessageSchema,
  type AssistantPage,
  AssistantPageSchema,
} from "~/lib/assistant/wire";
import type { WsClient } from "~/lib/ws-client";
import { define, QUICK } from "~/lib/ws-rpc";

/** How many messages a history page asks for. */
export const HISTORY_PAGE = 50;
/** How many journal entries the recent-updates strip reads. */
export const JOURNAL_LOOK = 50;

const sayRpc = define<unknown, { text: string }>("assistant.say", QUICK);
const historyRpc = define<unknown, { before?: string; limit?: number }>("assistant.history");
const journalRpc = define<unknown, { since?: string; limit?: number }>("assistant.journal");

/**
 * Sends the operator's text to the head and resolves with the stored ask.
 *
 * A malformed answer resolves to `undefined` rather than throwing: the message
 * is already stored server-side and arrives on the `assistant.message` push, so
 * the send did not fail just because its echo was unreadable.
 */
export async function say(ws: WsClient, text: string): Promise<AssistantMessage | undefined> {
  const raw = await sayRpc(ws, { text });
  const parsed = AssistantMessageSchema.safeParse(raw);
  return parsed.success ? parsed.data : undefined;
}

/** One page of the conversation, oldest first. Omit `before` for the newest. */
export async function history(
  ws: WsClient,
  before?: string,
  limit = HISTORY_PAGE,
): Promise<AssistantPage> {
  const raw = await historyRpc(ws, before ? { before, limit } : { limit });
  const parsed = AssistantPageSchema.safeParse(raw);
  if (!parsed.success) {
    throw new Error("assistant.history answered a shape this build cannot read");
  }
  return parsed.data;
}

/**
 * What has happened, newest first.
 *
 * A PURE read of the journal, which is why it can share the concurrent lane:
 * `since` is a timestamp the caller holds, not a per-surface seen mark. The
 * server's `SinceLast` — the half that stamps what a surface has looked at —
 * writes, so it belongs on no read lane and is not this op (see
 * docs/assistant.md, Build notes). Omitting `since` asks for the newest the
 * server will answer with, which is what the thread pins at its top.
 */
export async function journal(
  ws: WsClient,
  since?: string,
  limit = JOURNAL_LOOK,
): Promise<AssistantJournalEntry[]> {
  const raw = await journalRpc(ws, since ? { since, limit } : { limit });
  const parsed = AssistantJournalResultSchema.safeParse(raw);
  if (!parsed.success) {
    throw new Error("assistant.journal answered a shape this build cannot read");
  }
  // An object, not a bare array, because a top-level array cannot grow a field
  // (a cursor, a count) without a wire transition. `entries` absent is an empty
  // journal, not a broken answer.
  return parsed.data.entries ?? [];
}

/** Parses one pushed journal entry, or `undefined` if it is unreadable. */
export function parseJournalEntry(payload: unknown): AssistantJournalEntry | undefined {
  const parsed = AssistantJournalEntrySchema.safeParse(payload);
  return parsed.success ? parsed.data : undefined;
}
