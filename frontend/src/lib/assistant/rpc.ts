/**
 * The assistant's WS ops.
 *
 * `assistant.say`, `assistant.mark-seen`, `assistant.decide` and
 * `assistant.digest` are mutations; `assistant.history`, `assistant.journal`,
 * `assistant.unseen` and `assistant.proposals` are reads and run on the
 * socket's concurrent lane. Two of the mutations leave the serial lane on the
 * server through `handleRequestAsync` — decide performs a git operation and
 * digest is several hundred queries — so neither holds a stop or an approval
 * answer queued behind it. The say returns the ASK, not the answer — the
 * head's reply arrives as `assistant.delta` pushes and then one
 * `assistant.message`, because a blocking say would hold this connection's
 * whole mutation lane for the length of a turn.
 *
 * M4 adds the policies: `assistant.policies` is a read on the concurrent lane,
 * `assistant.policy-save` and `assistant.policy-delete` are mutations.
 */

import {
  type AssistantJournalEntry,
  AssistantJournalEntrySchema,
  AssistantJournalResultSchema,
  type AssistantMessage,
  AssistantMessageSchema,
  type AssistantPage,
  AssistantPageSchema,
  AssistantPoliciesResultSchema,
  type AssistantPolicy,
  AssistantPolicySchema,
  type AssistantProposal,
  AssistantProposalSchema,
  AssistantProposalsResultSchema,
  AssistantUnseenResultSchema,
} from "~/lib/assistant/wire";
import type { WsClient } from "~/lib/ws-client";
import { define, LONG, MEDIUM, QUICK } from "~/lib/ws-rpc";

/** How many messages a history page asks for. */
export const HISTORY_PAGE = 50;
/** How many journal entries the recent-updates strip reads. */
export const JOURNAL_LOOK = 50;
/** How many proposals a read asks for. The server's own cap is the same 50. */
export const PROPOSAL_PAGE = 50;

const sayRpc = define<unknown, { text: string }>("assistant.say", QUICK);
const historyRpc = define<unknown, { before?: string; limit?: number }>("assistant.history");
const journalRpc = define<unknown, { since?: string; limit?: number }>("assistant.journal");
const unseenRpc = define<unknown, Record<string, never>>("assistant.unseen", QUICK);
const markSeenRpc = define<unknown, Record<string, never>>("assistant.mark-seen", QUICK);
const proposalsRpc = define<unknown, { limit?: number }>("assistant.proposals", QUICK);
// A yes performs the action: a merge is five git subprocesses and a reclaim
// walks a worktree, so this gets the same budget as every other git mutation
// rather than the read lane's ten seconds. The server runs it through
// `handleRequestAsync` for the same reason.
const decideRpc = define<unknown, { id: string; accept: boolean }>("assistant.decide", LONG);
// A digest reads a brief for every session it names, which is several hundred
// queries on a busy machine: a single-generation budget, not a status poll's.
const digestRpc = define<unknown, Record<string, never>>("assistant.digest", MEDIUM);
// The standing instructions. The read is one short table on the concurrent
// lane; the two writes are upserts on the mutation lane, because a save and the
// push it broadcasts must not overtake each other.
const policiesRpc = define<unknown, Record<string, never>>("assistant.policies", QUICK);
const policySaveRpc = define<unknown, AssistantPolicySave>("assistant.policy-save", QUICK);
const policyDeleteRpc = define<unknown, { id: string }>("assistant.policy-delete", QUICK);

/**
 * What a save sends: the row's fields, and its `id` only when it already has
 * one. A blank row omits the id and the server mints it, which is what makes the
 * page's new row the same op as an edit rather than a second one.
 */
export interface AssistantPolicySave {
  id?: string;
  name: string;
  text: string;
  enabled: boolean;
  budgetInFlight: number;
  budgetPerDay: number;
}

/**
 * The standing instructions, as the server holds them.
 *
 * A pure read. An unreadable answer THROWS rather than resolving empty: an empty
 * list here would say the assistant is acting under no policy at all, which is
 * a claim about its autonomy and not a missing count.
 */
export async function policies(ws: WsClient): Promise<AssistantPolicy[]> {
  const raw = await policiesRpc(ws, {});
  const parsed = AssistantPoliciesResultSchema.safeParse(raw);
  if (!parsed.success) {
    throw new Error("assistant.policies answered a shape this build cannot read");
  }
  // An object rather than a bare array, so the answer can grow a field without
  // a wire transition; `policies` absent means none are set.
  return parsed.data.policies ?? [];
}

/**
 * Writes one policy and resolves with the row as the server now holds it.
 *
 * The answer is what the page renders, not what it sent: the server mints the
 * id, caps the text, clamps the budgets and stamps `updatedAt`, so a page that
 * kept its own copy would show a row nobody could act on. An unreadable answer
 * throws — a save whose result cannot be read is a save that cannot be
 * confirmed, and the push carries the row anyway.
 */
export async function savePolicy(
  ws: WsClient,
  policy: AssistantPolicySave,
): Promise<AssistantPolicy> {
  const raw = await policySaveRpc(ws, policy);
  const parsed = AssistantPolicySchema.safeParse(raw);
  if (!parsed.success) {
    throw new Error("assistant.policy-save answered a shape this build cannot read");
  }
  return parsed.data;
}

/** Removes one policy. The row leaves every surface on the `assistant.policy`
 *  push that carries `deleted: true`, so nothing is returned here. */
export async function deletePolicy(ws: WsClient, id: string): Promise<void> {
  await policyDeleteRpc(ws, { id });
}

/**
 * How many journal entries the thread has never been shown — the rail row's
 * notch, read once per connection. A pure read. An unreadable answer is zero:
 * a notch that cannot be counted is better off than a notch that lies.
 */
export async function unseen(ws: WsClient): Promise<number> {
  const raw = await unseenRpc(ws, {});
  const parsed = AssistantUnseenResultSchema.safeParse(raw);
  return parsed.success ? (parsed.data.count ?? 0) : 0;
}

/**
 * Tells the server the thread has shown what it holds. A WRITE — it stamps the
 * journal's seen marks and moves the thread's conversation mark — which is why
 * it is its own op on the mutation lane rather than a flag on the journal read.
 */
export async function markSeen(ws: WsClient): Promise<void> {
  await markSeenRpc(ws, {});
}

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

/**
 * What has been proposed, open first then decided, newest first.
 *
 * A pure read, on the socket's concurrent lane: expiry is applied on the server
 * as it reads, which is a derivation and not a write (docs/assistant.md, the M3
 * contract). An unreadable answer throws — unlike the unseen count, an empty
 * list here would claim there is nothing waiting for a yes.
 */
export async function proposals(ws: WsClient, limit = PROPOSAL_PAGE): Promise<AssistantProposal[]> {
  const raw = await proposalsRpc(ws, { limit });
  const parsed = AssistantProposalsResultSchema.safeParse(raw);
  if (!parsed.success) {
    throw new Error("assistant.proposals answered a shape this build cannot read");
  }
  // An object rather than a bare array, and `proposals` absent means none.
  return parsed.data.proposals ?? [];
}

/**
 * The yes or the no on one proposal, answering the row as it now stands.
 *
 * A MUTATION, and the one that performs an uncontained verb — accepting
 * re-checks the live facts server-side and either acts, or answers `stale`
 * having done nothing. So the caller renders what comes back rather than
 * assuming the press landed: a row that answers `stale` or `failed` is the
 * card's whole point.
 *
 * The surface is not a parameter. `decided_via` records WHERE a decision was
 * given, and a client that could name its own surface could file a card
 * somebody pressed on screen as a yes spoken on a call.
 */
export async function decide(
  ws: WsClient,
  id: string,
  accept: boolean,
): Promise<AssistantProposal> {
  const raw = await decideRpc(ws, { id, accept });
  const parsed = AssistantProposalSchema.safeParse(raw);
  if (!parsed.success) {
    throw new Error("assistant.decide answered a shape this build cannot read");
  }
  return parsed.data;
}

/**
 * Posts a digest now and resolves with it.
 *
 * The message is stored server-side and arrives on the `assistant.message`
 * push like any other, so an unreadable answer resolves to `undefined` rather
 * than throwing: the digest was composed, and only its echo was lost.
 */
export async function digest(ws: WsClient): Promise<AssistantMessage | undefined> {
  const raw = await digestRpc(ws, {});
  const parsed = AssistantMessageSchema.safeParse(raw);
  return parsed.success ? parsed.data : undefined;
}

/** Parses one pushed journal entry, or `undefined` if it is unreadable. */
export function parseJournalEntry(payload: unknown): AssistantJournalEntry | undefined {
  const parsed = AssistantJournalEntrySchema.safeParse(payload);
  return parsed.success ? parsed.data : undefined;
}
