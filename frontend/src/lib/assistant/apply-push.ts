/**
 * The assistant's six global pushes, applied to the store.
 *
 * They arrive on the primary's socket on the global topic (the conversation is
 * a project-less channel, which already fans out there), and there is one
 * assistant per primary — so unlike a session push there is nothing to route
 * and nothing to key by machine.
 *
 * Kept apart from the hook that subscribes them so the applying is testable
 * without a socket, on the `apply-event` seam's precedent. Each one parses
 * before it writes: a payload this build cannot read is dropped with a line in
 * the console, never half-applied.
 */

import {
  AssistantDeltaSchema,
  AssistantJournalEntrySchema,
  AssistantMessageSchema,
  AssistantPolicySchema,
  AssistantProposalSchema,
  AssistantStepPushSchema,
} from "~/lib/assistant/wire";
import { useAssistantStore } from "~/stores/assistant-store";

/** `assistant.message` — a stored turn, the operator's or the head's. */
export function applyAssistantMessage(payload: unknown): void {
  const parsed = AssistantMessageSchema.safeParse(payload);
  if (!parsed.success) {
    console.warn("[assistant] unreadable assistant.message", parsed.error.issues);
    return;
  }
  useAssistantStore.getState().appendMessage(parsed.data);
}

/** `assistant.delta` — the head's reply in progress, new text only. */
export function applyAssistantDelta(payload: unknown): void {
  const parsed = AssistantDeltaSchema.safeParse(payload);
  if (!parsed.success) {
    console.warn("[assistant] unreadable assistant.delta", parsed.error.issues);
    return;
  }
  // An empty delta still arms the gate: it says a reply is coming.
  useAssistantStore.getState().appendDelta(parsed.data.text ?? "");
}

/**
 * `assistant.step` — one thing the in-flight turn did, started or settled.
 * The stored message carries the finished list, so a push that cannot be read
 * costs a live row and nothing else.
 */
export function applyAssistantStep(payload: unknown): void {
  const parsed = AssistantStepPushSchema.safeParse(payload);
  if (!parsed.success) {
    console.warn("[assistant] unreadable assistant.step", parsed.error.issues);
    return;
  }
  if (!parsed.data.step) return;
  useAssistantStore.getState().applyStep(parsed.data.step);
}

/**
 * `assistant.journal` — one new entry.
 *
 * News that lands while the thread is on screen is seen as it arrives: the
 * store keeps the rail's count honest, and the page's installed look tells the
 * server so the stamp keeps pace with the strip.
 */
export function applyAssistantJournal(payload: unknown): void {
  const parsed = AssistantJournalEntrySchema.safeParse(payload);
  if (!parsed.success) {
    console.warn("[assistant] unreadable assistant.journal", parsed.error.issues);
    return;
  }
  const store = useAssistantStore.getState();
  const before = store.journal;
  store.addJournalEntry(parsed.data);
  const after = useAssistantStore.getState();
  if (after.viewing && after.journal !== before) after.look?.();
}

/**
 * `assistant.proposal` — one proposal row, on create and on every decision.
 *
 * The same push carries both, because both are the row as it now stands: a
 * create arrives `open` and a decision arrives `accepted`, `declined`, `stale`
 * or `failed`, and the store merges by id so the card the reader is looking at
 * becomes its own outcome rather than vanishing. Nothing is counted as unseen
 * here — a proposal's claim on attention is the card and the deck row, and the
 * `proposal_made` journal entry rides the journal push beside it.
 */
export function applyAssistantProposal(payload: unknown): void {
  const parsed = AssistantProposalSchema.safeParse(payload);
  if (!parsed.success) {
    console.warn("[assistant] unreadable assistant.proposal", parsed.error.issues);
    return;
  }
  useAssistantStore.getState().applyProposal(parsed.data);
}

/**
 * `assistant.policy` — one standing instruction, on save and on delete.
 *
 * The same push carries both, because both are the row as it now stands: a save
 * arrives whole and a delete arrives with `deleted: true`, which the store reads
 * as "drop this row". A delete is not an empty row — an empty row would be a
 * policy with no name, which is a thing the page would draw.
 */
export function applyAssistantPolicy(payload: unknown): void {
  const parsed = AssistantPolicySchema.safeParse(payload);
  if (!parsed.success) {
    console.warn("[assistant] unreadable assistant.policy", parsed.error.issues);
    return;
  }
  useAssistantStore.getState().applyPolicy(parsed.data);
}
