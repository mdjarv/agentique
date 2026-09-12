/**
 * The assistant's three global pushes, applied to the store.
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
