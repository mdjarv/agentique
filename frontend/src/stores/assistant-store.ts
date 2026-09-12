import { create } from "zustand";
import type { AssistantJournalEntry, AssistantMessage, AssistantPage } from "~/lib/assistant/wire";

/**
 * The assistant thread's client state.
 *
 * Two lists and one string, and the string is the interesting one: the
 * conversation is a channel, so a stored message is whole where a session's
 * turn streams, and the head's in-progress reply arrives as `assistant.delta`
 * pushes that this store accumulates until the finished `assistant.message`
 * replaces it. `streaming` is therefore both the text to render and the answer
 * to "is a reply in flight" — one writer, so the composer's disabled state
 * cannot disagree with what is on screen.
 *
 * Ordering is lexicographic on the wire stamps, which is chronological: the
 * conversation's own stamps are fixed-width nanoseconds and the journal's are
 * UTC RFC3339 seconds, both by construction (docs/assistant.md).
 */

/** Fallbacks are module-level so a selector never mints a new reference. */
export const EMPTY_MESSAGES: AssistantMessage[] = [];
export const EMPTY_JOURNAL: AssistantJournalEntry[] = [];

/** What identifies a journal entry when merging. */
function journalKey(entry: AssistantJournalEntry): string {
  if (entry.id !== undefined) return `id:${entry.id}`;
  // An entry from a peer that does not spell `id` is still not news twice: its
  // stamp plus its text is what a duplicate would repeat.
  return `at:${entry.at ?? ""}|${entry.kind ?? ""}|${entry.summary ?? ""}`;
}

function journalOrder(a: AssistantJournalEntry, b: AssistantJournalEntry): number {
  const at = (a.at ?? "").localeCompare(b.at ?? "");
  if (at !== 0) return at;
  return (a.id ?? 0) - (b.id ?? 0);
}

function messageOrder(a: AssistantMessage, b: AssistantMessage): number {
  const at = (a.createdAt ?? "").localeCompare(b.createdAt ?? "");
  if (at !== 0) return at;
  return (a.id ?? "").localeCompare(b.id ?? "");
}

/**
 * Whether two copies of one row say the same thing.
 *
 * Structural rather than by reference, because a look and a push carry the same
 * row as two objects, and that must not read as news. Stringified rather than
 * field-by-field so `payload` is covered too: a hand-written field list would
 * silently stop noticing whatever was added to the row next, and the reads are
 * bounded at 50 entries a look.
 */
function sameEntry(a: AssistantJournalEntry, b: AssistantJournalEntry): boolean {
  return a === b || JSON.stringify(a) === JSON.stringify(b);
}

/** Merges by identity and returns the list in order, oldest first. */
function mergeJournal(
  held: AssistantJournalEntry[],
  incoming: AssistantJournalEntry[],
): AssistantJournalEntry[] {
  if (incoming.length === 0) return held;
  const byKey = new Map<string, AssistantJournalEntry>();
  for (const entry of held) byKey.set(journalKey(entry), entry);
  let changed = false;
  for (const entry of incoming) {
    const key = journalKey(entry);
    const known = byKey.get(key);
    // Later wins: a re-read carries the row as it now stands — `notable` can
    // be set after the fact — so the copy is replaced, and only an IDENTICAL
    // row counts as nothing new.
    if (!known || !sameEntry(known, entry)) changed = true;
    byKey.set(key, entry);
  }
  // Nothing new arrived. Keep the held reference so a look that learns nothing
  // re-renders nothing.
  if (!changed) return held;
  return [...byKey.values()].sort(journalOrder);
}

/**
 * Merges the conversation by message id, oldest first.
 *
 * A message with no id cannot be identified, so it is kept rather than
 * deduped: the conversation's own writes always carry one, and losing a turn is
 * worse than showing it twice.
 */
function mergeMessages(held: AssistantMessage[], incoming: AssistantMessage[]): AssistantMessage[] {
  if (incoming.length === 0) return held;
  const byId = new Map<string, AssistantMessage>();
  const anonymous: AssistantMessage[] = [];
  let changed = false;
  for (const msg of held) {
    if (msg.id) byId.set(msg.id, msg);
    else anonymous.push(msg);
  }
  for (const msg of incoming) {
    if (!msg.id) {
      anonymous.push(msg);
      changed = true;
      continue;
    }
    const known = byId.get(msg.id);
    if (!known || JSON.stringify(known) !== JSON.stringify(msg)) changed = true;
    byId.set(msg.id, msg);
  }
  // A page that re-delivers what is already held leaves the reference alone.
  if (!changed) return held;
  return [...byId.values(), ...anonymous].sort(messageOrder);
}

interface AssistantState {
  /** The conversation, oldest first. */
  messages: AssistantMessage[];
  /** The journal, oldest first. The strip reads the tail of it. */
  journal: AssistantJournalEntry[];
  /**
   * The head's reply so far, or null when nothing is in flight. Also the
   * composer's gate: a reply is streaming exactly when this is a string.
   */
  streaming: string | null;
  /** The cursor for the page before the oldest message held, or "" at the start. */
  before: string;
  /** True once history has landed at least once, so the empty state is honest. */
  loaded: boolean;
  loading: boolean;
  /** Why the thread is empty, when it is empty for a reason. */
  error: string | null;
  /**
   * Journal entries the thread has never been shown. The rail row's notch is
   * `unseen > 0`; the number itself is only ever spoken to a screen reader.
   * Seeded from `assistant.unseen` at connect, bumped by each journal push
   * that lands while the thread is not on screen, zeroed by a look.
   */
  unseen: number;
  /** True while the thread page is mounted: a push that lands then is seen. */
  viewing: boolean;
  /**
   * How the page tells the server it has looked, installed while it is on
   * screen. A journal push that lands then is acknowledged through this, so
   * the stamp keeps pace with the strip without the page polling its own
   * store. Null when no thread is mounted.
   */
  look: (() => void) | null;

  setUnseen: (unseen: number) => void;
  setViewing: (viewing: boolean) => void;
  setLook: (look: (() => void) | null) => void;
  /** The thread has shown what it holds: the notch goes. */
  markSeen: () => void;
  setLoading: (loading: boolean) => void;
  setError: (error: string | null) => void;
  /** The newest page: replaces what is held. */
  setHistory: (page: AssistantPage) => void;
  /** An older page: merges under what is held and moves the cursor back. */
  prependHistory: (page: AssistantPage) => void;
  /** A read of the journal: merges what came back under what is held. */
  applyJournal: (entries: AssistantJournalEntry[]) => void;
  appendMessage: (message: AssistantMessage) => void;
  /**
   * The ask is away and the head owes an answer. Arms the gate before the
   * first delta arrives, because the composer must not accept a second message
   * during the seconds between the send and the head's first token.
   */
  beginReply: () => void;
  appendDelta: (text: string) => void;
  addJournalEntry: (entry: AssistantJournalEntry) => void;
  /** Drops the in-flight reply without storing it — a failed turn, a reconnect. */
  clearStreaming: () => void;
  reset: () => void;
}

export const useAssistantStore = create<AssistantState>((set) => ({
  messages: EMPTY_MESSAGES,
  journal: EMPTY_JOURNAL,
  streaming: null,
  before: "",
  loaded: false,
  loading: false,
  error: null,
  unseen: 0,
  viewing: false,
  look: null,

  setUnseen: (unseen) => set({ unseen: Math.max(0, unseen) }),
  setViewing: (viewing) => set({ viewing }),
  setLook: (look) => set({ look }),
  markSeen: () => set((s) => (s.unseen === 0 ? s : { unseen: 0 })),
  setLoading: (loading) => set({ loading }),
  setError: (error) => set({ error }),

  setHistory: (page) =>
    set((s) => ({
      // A fresh newest page is authoritative for what it covers, but a message
      // pushed while it was in flight must not be lost behind it.
      messages: mergeMessages(s.messages, page.messages ?? []),
      before: page.before ?? "",
      loaded: true,
      loading: false,
      error: null,
    })),

  prependHistory: (page) =>
    set((s) => ({
      messages: mergeMessages(s.messages, page.messages ?? []),
      before: page.before ?? "",
      loading: false,
    })),

  applyJournal: (entries) => set((s) => ({ journal: mergeJournal(s.journal, entries) })),

  appendMessage: (message) =>
    set((s) => ({
      messages: mergeMessages(s.messages, [message]),
      // The stored reply replaces the partial one. Only the head's own turn
      // ends the stream: the operator's ask arrives on the same push.
      streaming: message.role === "assistant" ? null : s.streaming,
    })),

  beginReply: () => set((s) => (s.streaming === null ? { streaming: "" } : s)),

  appendDelta: (text) =>
    set((s) => ({
      // The delta carries the NEW text, not the whole reply so far.
      streaming: (s.streaming ?? "") + text,
    })),

  addJournalEntry: (entry) =>
    set((s) => {
      const journal = mergeJournal(s.journal, [entry]);
      // A push the merge already held is not news twice, and one that lands
      // while the thread is on screen is seen as it arrives — the page stamps
      // the look; this only keeps the count honest for the row.
      const isNew = journal !== s.journal;
      return {
        journal,
        unseen: isNew && !s.viewing ? s.unseen + 1 : s.unseen,
      };
    }),

  clearStreaming: () => set({ streaming: null }),

  reset: () =>
    set({
      messages: EMPTY_MESSAGES,
      journal: EMPTY_JOURNAL,
      streaming: null,
      before: "",
      loaded: false,
      loading: false,
      error: null,
      unseen: 0,
      viewing: false,
      look: null,
    }),
}));

/**
 * Selectors, exported so every reader shares one reference-stable function.
 * Each returns something already in the store or a primitive — never a fresh
 * array, object or `.map()` result, which is the rule that keeps a subscriber
 * from re-rendering forever.
 */
export const selectAssistantMessages = (s: AssistantState) => s.messages;
export const selectAssistantJournal = (s: AssistantState) => s.journal;
export const selectAssistantStreaming = (s: AssistantState) => s.streaming;
/** A reply is in flight exactly while the head has streamed something. */
export const selectAssistantReplying = (s: AssistantState) => s.streaming !== null;
export const selectAssistantLoaded = (s: AssistantState) => s.loaded;
export const selectAssistantLoading = (s: AssistantState) => s.loading;
export const selectAssistantError = (s: AssistantState) => s.error;
export const selectAssistantBefore = (s: AssistantState) => s.before;
export const selectAssistantUnseen = (s: AssistantState) => s.unseen;
export const selectAssistantViewing = (s: AssistantState) => s.viewing;
