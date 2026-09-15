import { create } from "zustand";
import { hasSteps, mergeStep } from "~/lib/assistant/steps";
import {
  type AssistantJournalEntry,
  type AssistantMessage,
  type AssistantPage,
  type AssistantPolicy,
  type AssistantProposal,
  type AssistantStep,
  DAY_SUMMARY_KIND,
  HEARTBEAT_KIND,
  isOpenProposal,
} from "~/lib/assistant/wire";

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
export const EMPTY_STEPS: AssistantStep[] = [];
export const EMPTY_JOURNAL: AssistantJournalEntry[] = [];
export const EMPTY_PROPOSALS: AssistantProposal[] = [];
export const EMPTY_POLICIES: AssistantPolicy[] = [];

/**
 * Whether an entry may raise the rail row's notch.
 *
 * Everything does except two kinds, and the server's
 * `CountAssistantJournalUnseen` leaves the same two out — one rule, both sides,
 * or a notch drawn live would disagree with the count the next connection
 * answers.
 *
 * A tick that woke up, looked and decided nothing (`heartbeat`) is not a claim
 * on anybody's attention; what it *did* has its own entry beside it. A folded
 * day (`day_summary`) is not one either, for the opposite reason: it is stamped
 * at the day it is about, so it sorts below everything the thread renders and a
 * fold of thirty days would claim thirty unread things with nothing new to look
 * at. The `compaction` row that records the fold is the news, and it counts.
 * Neither is drawn in the thread's timeline (`buildTimeline` leaves both out).
 */
function claimsAttention(entry: AssistantJournalEntry): boolean {
  return entry.kind !== HEARTBEAT_KIND && entry.kind !== DAY_SUMMARY_KIND;
}

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

/**
 * Proposals, open first and then newest first — the order the server's own list
 * answers in, because an open row is a claim on attention and a decided one is
 * a record.
 */
function proposalOrder(a: AssistantProposal, b: AssistantProposal): number {
  const openness = Number(isOpenProposal(b.status)) - Number(isOpenProposal(a.status));
  if (openness !== 0) return openness;
  const at = (b.createdAt ?? "").localeCompare(a.createdAt ?? "");
  if (at !== 0) return at;
  return (a.id ?? "").localeCompare(b.id ?? "");
}

/**
 * Merges proposals by id.
 *
 * Later wins: a row arrives open on the create push and decided on the decide
 * push, and the decided copy is the one a card must render. A row with no id
 * cannot be identified, so it is DROPPED rather than kept — unlike a message,
 * an unidentifiable proposal is one nothing could ever decide, and rendering
 * Accept on it would offer a press that cannot be sent.
 */
function mergeProposals(
  held: AssistantProposal[],
  incoming: AssistantProposal[],
): AssistantProposal[] {
  if (incoming.length === 0) return held;
  const byId = new Map<string, AssistantProposal>();
  for (const row of held) if (row.id) byId.set(row.id, row);
  let changed = false;
  for (const row of incoming) {
    if (!row.id) continue;
    const known = byId.get(row.id);
    if (!known || JSON.stringify(known) !== JSON.stringify(row)) changed = true;
    byId.set(row.id, row);
  }
  // A read that re-delivers what is already held leaves the reference alone, so
  // the deck and the thread re-render nothing.
  if (!changed) return held;
  return [...byId.values()].sort(proposalOrder);
}

/**
 * Policies by name, then id — the order the page is read down, and the only
 * ordering a standing instruction has: there is no recency to a rule that is
 * always in force, and sorting by `updatedAt` would move the row under somebody
 * mid-edit every time they saved a neighbour.
 */
function policyOrder(a: AssistantPolicy, b: AssistantPolicy): number {
  const name = (a.name ?? "").localeCompare(b.name ?? "");
  if (name !== 0) return name;
  return (a.id ?? "").localeCompare(b.id ?? "");
}

/**
 * Merges policies by id, and REMOVES the ones a push marks deleted.
 *
 * `deleted` is a push-only field: the server sends the row it removed so every
 * surface can drop it, where a list read never carries one. A row with no id is
 * dropped for the reason an unidentifiable proposal is — the page would draw
 * Save and Delete on something neither op could name.
 */
function mergePolicies(held: AssistantPolicy[], incoming: AssistantPolicy[]): AssistantPolicy[] {
  if (incoming.length === 0) return held;
  const byId = new Map<string, AssistantPolicy>();
  for (const row of held) if (row.id) byId.set(row.id, row);
  let changed = false;
  for (const row of incoming) {
    if (!row.id) continue;
    if (row.deleted) {
      changed = byId.delete(row.id) || changed;
      continue;
    }
    const known = byId.get(row.id);
    if (!known || JSON.stringify(known) !== JSON.stringify(row)) changed = true;
    byId.set(row.id, row);
  }
  // A read that re-delivers what is already held leaves the reference alone.
  if (!changed) return held;
  if (byId.size === 0) return EMPTY_POLICIES;
  return [...byId.values()].sort(policyOrder);
}

/**
 * A whole list read REPLACES what is held, unlike a merge.
 *
 * A policy that has been deleted from another tab is gone from the answer and
 * from nowhere else: with no per-row tombstone in a list, merging would keep a
 * row the server no longer has, and every Save on it would fail.
 */
function replacePolicies(held: AssistantPolicy[], rows: AssistantPolicy[]): AssistantPolicy[] {
  const next = rows.filter((row) => !!row.id && !row.deleted).sort(policyOrder);
  if (next.length === 0) return held.length === 0 ? held : EMPTY_POLICIES;
  if (
    next.length === held.length &&
    next.every((row, i) => JSON.stringify(row) === JSON.stringify(held[i]))
  ) {
    return held;
  }
  return next;
}

/**
 * The open subset, STORED rather than derived in a selector.
 *
 * `.filter()` in a selector mints a new array on every call, which is the rule
 * that re-renders a subscriber forever — and this list has two subscribers (the
 * thread and the deck). Computed once per write, and the previous reference is
 * kept when the subset is unchanged.
 */
function openOf(all: AssistantProposal[], previous: AssistantProposal[]): AssistantProposal[] {
  const open = all.filter((row) => isOpenProposal(row.status));
  if (open.length === 0) return previous.length === 0 ? previous : EMPTY_PROPOSALS;
  if (open.length === previous.length && open.every((row, i) => row === previous[i])) {
    return previous;
  }
  return open;
}

interface AssistantState {
  /** The conversation, oldest first. */
  messages: AssistantMessage[];
  /** The journal, oldest first. The thread merges it into its timeline. */
  journal: AssistantJournalEntry[];
  /**
   * Proposals, open first then newest first — decided ones included, because a
   * card the reader just pressed has to be able to say what happened.
   */
  proposals: AssistantProposal[];
  /** The open ones, kept as their own array so a subscriber can read it. */
  openProposals: AssistantProposal[];
  /**
   * The standing instructions the heartbeat may act under, by name. Seeded once
   * per connection and kept current by the `assistant.policy` push, so the
   * policies page is right whichever tab was used to change them.
   */
  policies: AssistantPolicy[];
  /**
   * The head's reply so far, or null when nothing is in flight. Also the
   * composer's gate: a reply is streaming exactly when this is a string.
   */
  streaming: string | null;
  /**
   * What the in-flight turn has done so far, from `assistant.step` pushes.
   *
   * Never the gate — `streaming` stays the one answer to "is a reply owed" —
   * because a heartbeat turn pushes steps with nobody's ask behind it, and a
   * silent one ends without the assistant message that releases the composer.
   * Rendered only beside the streaming reply, and dropped when the turn's
   * stored message lands carrying the finished list.
   */
  liveSteps: AssistantStep[];
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
   * the stamp keeps pace with the timeline without the page polling its own
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
  /** A read of the proposals: merges what came back under what is held. */
  applyProposals: (rows: AssistantProposal[]) => void;
  /** One row, from the push or from the answer to a decide. Merges by id. */
  applyProposal: (row: AssistantProposal) => void;
  /** A read of the policies: the answer is the whole set, so it replaces. */
  setPolicies: (rows: AssistantPolicy[]) => void;
  /** One row, from a save's answer or the push. `deleted` removes it. */
  applyPolicy: (row: AssistantPolicy) => void;
  appendMessage: (message: AssistantMessage) => void;
  /**
   * The ask is away and the head owes an answer. Arms the gate before the
   * first delta arrives, because the composer must not accept a second message
   * during the seconds between the send and the head's first token.
   */
  beginReply: () => void;
  appendDelta: (text: string) => void;
  /** One step started or settled; merges by `seq`. */
  applyStep: (step: AssistantStep) => void;
  addJournalEntry: (entry: AssistantJournalEntry) => void;
  /** Drops the in-flight reply without storing it — a failed turn, a reconnect. */
  clearStreaming: () => void;
  reset: () => void;
}

export const useAssistantStore = create<AssistantState>((set) => ({
  messages: EMPTY_MESSAGES,
  journal: EMPTY_JOURNAL,
  proposals: EMPTY_PROPOSALS,
  openProposals: EMPTY_PROPOSALS,
  policies: EMPTY_POLICIES,
  streaming: null,
  liveSteps: EMPTY_STEPS,
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

  applyProposals: (rows) =>
    set((s) => {
      const proposals = mergeProposals(s.proposals, rows);
      if (proposals === s.proposals) return s;
      return { proposals, openProposals: openOf(proposals, s.openProposals) };
    }),

  applyProposal: (row) =>
    set((s) => {
      const proposals = mergeProposals(s.proposals, [row]);
      if (proposals === s.proposals) return s;
      return { proposals, openProposals: openOf(proposals, s.openProposals) };
    }),

  setPolicies: (rows) =>
    set((s) => {
      const policies = replacePolicies(s.policies, rows);
      return policies === s.policies ? s : { policies };
    }),

  applyPolicy: (row) =>
    set((s) => {
      const policies = mergePolicies(s.policies, [row]);
      return policies === s.policies ? s : { policies };
    }),

  appendMessage: (message) =>
    set((s) => {
      const endsTurn = message.role === "assistant";
      return {
        messages: mergeMessages(s.messages, [message]),
        // The stored reply replaces the partial one. Only the head's own turn
        // ends the stream: the operator's ask arrives on the same push.
        streaming: endsTurn ? null : s.streaming,
        // The live list goes with the turn whose record just landed — the
        // reply, or a heartbeat wake-up that took the steps of a turn that
        // wrote none.
        liveSteps: endsTurn || hasSteps(message) ? EMPTY_STEPS : s.liveSteps,
      };
    }),

  beginReply: () =>
    set((s) => (s.streaming === null ? { streaming: "", liveSteps: EMPTY_STEPS } : s)),

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
        unseen: isNew && !s.viewing && claimsAttention(entry) ? s.unseen + 1 : s.unseen,
      };
    }),

  applyStep: (step) =>
    set((s) => {
      const liveSteps = mergeStep(s.liveSteps, step);
      return liveSteps === s.liveSteps ? s : { liveSteps };
    }),

  clearStreaming: () => set({ streaming: null, liveSteps: EMPTY_STEPS }),

  reset: () =>
    set({
      messages: EMPTY_MESSAGES,
      journal: EMPTY_JOURNAL,
      proposals: EMPTY_PROPOSALS,
      openProposals: EMPTY_PROPOSALS,
      policies: EMPTY_POLICIES,
      streaming: null,
      liveSteps: EMPTY_STEPS,
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
export const selectAssistantProposals = (s: AssistantState) => s.proposals;
/** The rows still waiting on somebody — the thread's cards and the deck's. */
export const selectAssistantOpenProposals = (s: AssistantState) => s.openProposals;
/** The standing instructions — the policies page and nothing else reads it. */
export const selectAssistantPolicies = (s: AssistantState) => s.policies;
export const selectAssistantStreaming = (s: AssistantState) => s.streaming;
export const selectAssistantLiveSteps = (s: AssistantState) => s.liveSteps;
/** A reply is in flight exactly while the head has streamed something. */
export const selectAssistantReplying = (s: AssistantState) => s.streaming !== null;
export const selectAssistantLoaded = (s: AssistantState) => s.loaded;
export const selectAssistantLoading = (s: AssistantState) => s.loading;
export const selectAssistantError = (s: AssistantState) => s.error;
export const selectAssistantBefore = (s: AssistantState) => s.before;
export const selectAssistantUnseen = (s: AssistantState) => s.unseen;
export const selectAssistantViewing = (s: AssistantState) => s.viewing;
