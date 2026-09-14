/**
 * The thread as one timeline: the conversation, the journal and the proposal
 * cards, in the order they happened.
 *
 * The page used to stack three scrollers in one column — a recent-updates band,
 * a proposals band, then the conversation in whatever height was left — so the
 * thing the page is for got a third of it, and a wheel over the bands scrolled a
 * different list. Merging them is the whole fix (design round 2026-09-14, option
 * A), and this is the merge. Pure, so the rules below are tested without a DOM.
 *
 * **Time decides the order, parsed rather than compared.** The conversation's
 * stamps carry nanoseconds and the journal's are whole seconds, so a string
 * comparison is right only to the second. Ties keep the order the three sources
 * were listed in, which is stable.
 *
 * **What the timeline leaves out, and why.**
 * - `heartbeat` rows are the assistant's bookkeeping; an acting tick already
 *   has its divider in the conversation, and the rest decided nothing.
 * - `day_summary` rows are stamped at the day they describe, a fortnight back,
 *   so they would surface at the top of a conversation they have nothing to do
 *   with. `claimsAttention` in the store leaves both out for the same reasons.
 * - `proposal_made` and `proposal_decided` for a proposal the client holds:
 *   the card IS that news, at its own time, and it shows its outcome.
 *
 * **A digest absorbs the updates it retells.** The digest message is the
 * journal grouped since the previous digest, so the raw rows between the two
 * fold under it instead of repeating it line by line. Only event rows are
 * absorbed — never a message or a card, which are not what a digest lists.
 *
 * **A run of three or more updates folds into one line**, except the newest
 * few of a run that is still news — after the "since you last looked" divider,
 * or past the last turn. Hiding what the visit is meant to show defeats it, and
 * showing fifty lines of it is the band this replaced.
 */

import type {
  AssistantJournalEntry,
  AssistantMessage,
  AssistantProposal,
} from "~/lib/assistant/wire";
import { DAY_SUMMARY_KIND, HEARTBEAT_KIND } from "~/lib/assistant/wire";

/** The message kind the core stamps on a digest (`messageKindDigest`). */
export const DIGEST_KIND = "digest";

/** The shortest run of adjacent updates that folds into one line. */
export const FOLD_MIN = 3;

/** How many of the newest updates stay open at the end and after the divider. */
export const SHOWN_AT_END = 5;

export type TimelineItem =
  | { type: "message"; key: string; message: AssistantMessage }
  | { type: "event"; key: string; entry: AssistantJournalEntry }
  | { type: "fold"; key: string; entries: AssistantJournalEntry[] }
  | {
      type: "digest";
      key: string;
      message: AssistantMessage;
      entries: AssistantJournalEntry[];
    }
  | { type: "proposal"; key: string; proposal: AssistantProposal }
  | { type: "unseen"; key: string };

export interface TimelineInput {
  messages: AssistantMessage[];
  journal: AssistantJournalEntry[];
  proposals: AssistantProposal[];
  /**
   * How many attention-claiming journal entries were unseen when the reader
   * arrived. The divider goes above the oldest of them; zero draws none.
   */
  unseenOnArrival: number;
}

interface Stamped {
  ms: number;
  order: number;
  item: TimelineItem;
}

export function buildTimeline({
  messages,
  journal,
  proposals,
  unseenOnArrival,
}: TimelineInput): TimelineItem[] {
  const held = new Set<string>();
  for (const p of proposals) if (p.id) held.add(p.id);

  const stamped: Stamped[] = [];
  let order = 0;
  const push = (at: string | undefined, item: TimelineItem) => {
    stamped.push({ ms: stampMs(at), order: order++, item });
  };

  messages.forEach((message, index) => {
    push(message.createdAt, {
      type: "message",
      key: message.id ? `m:${message.id}` : `m:${message.createdAt ?? ""}:${index}`,
      message,
    });
  });
  for (const proposal of proposals) {
    if (!proposal.id) continue;
    push(proposal.createdAt, { type: "proposal", key: `p:${proposal.id}`, proposal });
  }
  journal.forEach((entry, index) => {
    if (!showsInTimeline(entry, held)) return;
    push(entry.at, { type: "event", key: entryKey(entry, index), entry });
  });

  stamped.sort((a, b) => a.ms - b.ms || a.order - b.order);

  const absorbed = absorbIntoDigests(stamped);
  const divided = insertUnseenDivider(absorbed, journal, unseenOnArrival);
  return foldRuns(divided);
}

/**
 * The local clock time of a wire stamp, or "" when it cannot be read.
 *
 * Empty rather than a guess: a row with no time still says what happened, where
 * "Invalid Date" says the app is broken.
 */
export function clockTime(iso?: string): string {
  if (!iso) return "";
  const ms = Date.parse(iso);
  if (Number.isNaN(ms)) return "";
  return new Date(ms).toLocaleTimeString(undefined, { hour: "2-digit", minute: "2-digit" });
}

/** A stamp as milliseconds; an unreadable one sorts first rather than throwing. */
function stampMs(at: string | undefined): number {
  if (!at) return 0;
  const ms = Date.parse(at);
  return Number.isNaN(ms) ? 0 : ms;
}

function entryKey(entry: AssistantJournalEntry, index: number): string {
  return entry.id !== undefined ? `j:${entry.id}` : `j:${entry.at ?? ""}:${index}`;
}

function showsInTimeline(entry: AssistantJournalEntry, held: Set<string>): boolean {
  if (entry.kind === HEARTBEAT_KIND || entry.kind === DAY_SUMMARY_KIND) return false;
  if (entry.kind === "proposal_made" || entry.kind === "proposal_decided") {
    const id = entry.payload?.proposalId;
    return !(typeof id === "string" && held.has(id));
  }
  return true;
}

/**
 * Moves the event rows before each digest message under it, back to the
 * previous digest. Everything else keeps its place.
 */
function absorbIntoDigests(stamped: Stamped[]): Stamped[] {
  const out: Stamped[] = [];
  let pending: { index: number; entry: AssistantJournalEntry }[] = [];
  for (const s of stamped) {
    const { item } = s;
    if (item.type === "event") {
      pending.push({ index: out.length, entry: item.entry });
      out.push(s);
      continue;
    }
    if (item.type === "message" && item.message.kind === DIGEST_KIND) {
      const taken = new Set(pending.map((p) => p.index));
      const kept = out.filter((_, i) => !taken.has(i));
      out.length = 0;
      out.push(...kept, {
        ...s,
        item: {
          type: "digest",
          key: item.key,
          message: item.message,
          entries: pending.map((p) => p.entry),
        },
      });
      pending = [];
      continue;
    }
    out.push(s);
  }
  return out;
}

/**
 * Puts the divider above the first item at or after the oldest entry the reader
 * had not seen. Counted over the whole journal, not the rows the timeline
 * shows, because the count it is matched against is the server's.
 */
function insertUnseenDivider(
  stamped: Stamped[],
  journal: AssistantJournalEntry[],
  unseen: number,
): TimelineItem[] {
  const items = stamped.map((s) => s.item);
  if (unseen <= 0) return items;
  const claiming = journal.filter((e) => e.kind !== HEARTBEAT_KIND && e.kind !== DAY_SUMMARY_KIND);
  if (claiming.length === 0) return items;
  const oldestUnseen = claiming[Math.max(0, claiming.length - unseen)];
  const from = stampMs(oldestUnseen?.at);
  const at = stamped.findIndex((s) => s.ms >= from);
  if (at === -1) return items;
  return [...items.slice(0, at), { type: "unseen", key: "unseen" }, ...items.slice(at)];
}

/**
 * Folds runs of adjacent event rows.
 *
 * A run in the middle of the conversation folds whole: it is history, and the
 * turns around it are what the reader scrolls back for. A run the reader has
 * not seen (after the divider), or one past the last thing anybody said (the
 * news right now), keeps its newest rows open and folds only what is ahead of
 * them — so a busy day is one fold and a few lines, not a screen of lines.
 */
function foldRuns(items: TimelineItem[]): TimelineItem[] {
  // The end of the conversation is past the last turn and past the divider: a
  // run ahead of the divider is news the reader has already seen.
  const lastBreak = items.findLastIndex(
    (i) => i.type === "message" || i.type === "digest" || i.type === "unseen",
  );
  const out: TimelineItem[] = [];
  let run: Extract<TimelineItem, { type: "event" }>[] = [];
  let pastDivider = false;
  const fold = (events: typeof run) => {
    const first = events[0];
    if (first)
      out.push({ type: "fold", key: `f:${first.key}`, entries: events.map((r) => r.entry) });
  };
  const flush = (index: number) => {
    const live = pastDivider || index > lastBreak;
    if (live) {
      const ahead = run.length - SHOWN_AT_END;
      if (ahead >= FOLD_MIN) {
        fold(run.slice(0, ahead));
        out.push(...run.slice(ahead));
      } else {
        out.push(...run);
      }
    } else if (run.length >= FOLD_MIN) {
      fold(run);
    } else {
      out.push(...run);
    }
    run = [];
  };
  items.forEach((item, index) => {
    if (item.type === "event") {
      run.push(item);
      return;
    }
    flush(index);
    if (item.type === "unseen") pastDivider = true;
    out.push(item);
  });
  flush(items.length);
  return out;
}
