/**
 * One glyph and one word per journal kind — the closed table every surface that
 * renders an entry reads, on `REST_GLYPH`'s precedent: a kind that says
 * "finished" in the strip cannot say something else in a digest.
 *
 * `untrusted` is not in here, because it is not a kind: it is a property of the
 * row, and what it changes is the FRAMING (a quotation, and a visible "reported
 * by" marker), not the mark.
 */

import {
  Archive,
  CalendarDays,
  Check,
  FoldVertical,
  GitMerge,
  Handshake,
  HeartPulse,
  type LucideIcon,
  Pause,
  Plus,
  Quote,
  SendHorizonal,
  StickyNote,
  TriangleAlert,
  X,
} from "lucide-react";
import type { AssistantJournalKind } from "~/lib/assistant/wire";

export interface JournalMark {
  glyph: LucideIcon;
  /** The word, lower case — the strip prints it as a lead-in, not a heading. */
  label: string;
  /** Tailwind text colour token for the glyph. */
  tone: string;
}

const MARKS: Record<AssistantJournalKind, JournalMark> = {
  session_finished: { glyph: Check, label: "finished", tone: "text-success" },
  session_failed: { glyph: X, label: "failed", tone: "text-destructive" },
  // The triangle means "someone is waiting on you", which is exactly what a
  // blocked run is. One mark, one meaning, across surfaces.
  session_blocked: { glyph: TriangleAlert, label: "blocked", tone: "text-warning" },
  session_merged: { glyph: GitMerge, label: "merged", tone: "text-muted-foreground" },
  session_archived: { glyph: Archive, label: "archived", tone: "text-muted-foreground" },
  loop_paused: { glyph: Pause, label: "loop paused", tone: "text-warning" },
  report: { glyph: Quote, label: "reported", tone: "text-agent" },
  dispatched: { glyph: SendHorizonal, label: "dispatched", tone: "text-primary" },
  session_created: { glyph: Plus, label: "created", tone: "text-primary" },
  note: { glyph: StickyNote, label: "note", tone: "text-muted-foreground" },
  day_summary: { glyph: CalendarDays, label: "that day", tone: "text-muted-foreground" },
  // The triangle again, and for the same reason: a proposal made is something
  // waiting on the operator, which is the one thing that mark means anywhere in
  // this app. The card and the deck row wear it too.
  proposal_made: { glyph: TriangleAlert, label: "proposed", tone: "text-warning" },
  // A decision is not waiting on anybody, so it drops the triangle. The
  // handshake is the strip's only claim here: what happened is in the summary,
  // which carries the verb, the target and the outcome the server wrote.
  proposal_decided: { glyph: Handshake, label: "decided", tone: "text-muted-foreground" },
  // The assistant woke by itself and judged what it found. Nobody is waiting on
  // it and nothing failed, so it takes the quietest tone on the table: what the
  // tick decided is in the summary, and what it DID has its own entry beside
  // this one (a dispatch, a proposal, a session created).
  heartbeat: { glyph: HeartPulse, label: "heartbeat", tone: "text-muted-foreground" },
  // A day folded away. It reads like a note — the server's own sentence, nothing
  // waiting on anybody — and takes the same quiet tone, because what it reports
  // is housekeeping on entries a fortnight old. The glyph is the one thing that
  // is specific: `day_summary` is the day that was kept, and this is the fold
  // that kept it, so the two must not wear the same mark.
  compaction: { glyph: FoldVertical, label: "compacted", tone: "text-muted-foreground" },
};

const UNKNOWN: JournalMark = {
  glyph: StickyNote,
  label: "happened",
  tone: "text-muted-foreground",
};

/**
 * The mark for a kind. A kind this build has never heard of gets the neutral
 * one rather than nothing: a server one release ahead is news the reader should
 * still see, and an entry with no mark reads as a rendering bug.
 */
export function journalMark(kind: string | undefined): JournalMark {
  if (!kind) return UNKNOWN;
  return MARKS[kind as AssistantJournalKind] ?? UNKNOWN;
}
