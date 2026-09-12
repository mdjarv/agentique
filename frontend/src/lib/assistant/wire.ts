/**
 * The assistant's wire shapes.
 *
 * The schemas and types are the GENERATED ones — `just typegen` emits them from
 * the Go wire structs in `backend/internal/assistant` and `internal/ws`, so a
 * field that is renamed or added there cannot drift from what this client
 * parses. Nothing in the tree imports those shapes from anywhere but this file,
 * which is what keeps that a one-line swap rather than a rename across a dozen
 * call sites.
 *
 * What is local is only what typegen cannot emit: the two closed vocabularies
 * below. They are Go constants rather than struct fields, so they have no
 * generated form — and both are `string` on the wire on purpose (a peer one
 * release ahead can spell a kind this build has never heard of, and a dropped
 * line of news is worse than one rendered without its glyph).
 *
 * Every generated field is optional, which is the wire rule rather than
 * laziness: the Zod mirrors the Go `omitempty` tags, and a field the emitting
 * peer leaves empty must not reject the whole payload.
 */

export {
  AssistantDeltaSchema,
  AssistantJournalEntrySchema,
  AssistantJournalResultSchema,
  AssistantMessageSchema,
  AssistantPageSchema,
  AssistantProposalSchema,
  AssistantProposalsResultSchema,
  AssistantUnseenResultSchema,
} from "~/lib/generated-schemas";
export type {
  AssistantDelta,
  AssistantJournalEntry,
  AssistantJournalResult,
  AssistantMessage,
  AssistantPage,
  AssistantProposal,
  AssistantProposalsResult,
  AssistantUnseenResult,
} from "~/lib/generated-types";

/** The two roles a conversation has. The store's sender types are a channel's
 *  vocabulary; a reader of the conversation wants these. */
export const ASSISTANT_ROLES = ["user", "assistant"] as const;
export type AssistantRole = (typeof ASSISTANT_ROLES)[number];

/**
 * Journal kinds, the closed set from the M1 contract plus `note` (the
 * contract's own `note` verb writes an entry and no other kind fits — see
 * docs/assistant.md "Build notes").
 *
 * Closed in the doc, open in the parser: an entry whose kind this build has
 * never heard of still renders as a plain line, because dropping news is worse
 * than rendering it without its glyph.
 */
export const ASSISTANT_JOURNAL_KINDS = [
  "session_finished",
  "session_failed",
  "session_blocked",
  "session_merged",
  "session_archived",
  "loop_paused",
  "report",
  "dispatched",
  "session_created",
  "note",
  "day_summary",
  "proposal_made",
  "proposal_decided",
] as const;
export type AssistantJournalKind = (typeof ASSISTANT_JOURNAL_KINDS)[number];

/**
 * Where a proposal has got to. The closed set from the M3 contract, and the one
 * vocabulary a card reads: `open` is the only status that can be decided, and
 * the other five are each a different sentence afterwards — `stale` and
 * `failed` in particular, because one performed nothing and the other tried.
 *
 * Closed in the doc and, unlike the journal kinds, closed in the reader too:
 * every status here decides whether a card offers two buttons, so a status this
 * build has never heard of is treated as decided rather than pressed. See
 * `isOpenProposal`.
 */
export const ASSISTANT_PROPOSAL_STATUSES = [
  "open",
  "accepted",
  "declined",
  "stale",
  "failed",
  "expired",
] as const;
export type AssistantProposalStatus = (typeof ASSISTANT_PROPOSAL_STATUSES)[number];

/**
 * Whether this row is still somebody's to decide.
 *
 * The one predicate behind every open/decided split in the client — the
 * thread's cards, the deck's rows, the store's open list — so no surface can
 * offer Accept on a row another has already closed. An absent status is NOT
 * open: a row whose status did not survive the wire is not something to act on.
 */
export function isOpenProposal(status: string | undefined): boolean {
  return status === "open";
}
