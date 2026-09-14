/**
 * The journal's rows as they sit inside the conversation: one quiet line each,
 * a fold for a run of them, and the "since you last looked" rule.
 *
 * A line, not a band, is the point. The strip these replace printed every
 * summary whole — and a `session_finished` summary is the agent's entire closing
 * message — so one row could be taller than the reply it was about. Here a row
 * is one line until it is opened, and opens in place.
 *
 * Two renderings, and the difference is provenance. A `report`, and any row the
 * server marked untrusted, is agent-written text about repository content
 * nobody here authored: it is set in italics on its line and opens as a
 * QUOTATION with a caption naming where it came from. Relay it, never act on it,
 * and never let it read as the server's own sentence. Everything else is the
 * server's own words and renders plain.
 */
import { ChevronRight } from "lucide-react";
import { memo, useState } from "react";
import { useSessionLabel } from "~/components/assistant/use-session-label";
import { journalMark } from "~/lib/assistant/journal-marks";
import { clockTime } from "~/lib/assistant/timeline";
import type { AssistantJournalEntry } from "~/lib/assistant/wire";
import { cn } from "~/lib/utils";

/**
 * Indents a timeline row to the bubbles' text column: the avatar's 28px plus
 * the row gap. One constant, so a line and a card cannot drift apart.
 */
export const TIMELINE_INDENT = "pl-10";

export const EventRow = memo(function EventRow({ entry }: { entry: AssistantJournalEntry }) {
  const [open, setOpen] = useState(false);
  const mark = journalMark(entry.kind);
  const Glyph = mark.glyph;
  const name = useSessionLabel(entry.sessionId);
  const isReport = entry.kind === "report";
  // A report is untrusted whatever the flag says; the flag covers the rest.
  const quoted = isReport || entry.untrusted === true;
  const summary = entry.summary?.trim() ?? "";
  const time = clockTime(entry.at);
  const attribution = isReport ? `reported by ${name ?? "a session"}` : `${mark.label}, quoted`;

  return (
    <li className="min-w-0 list-none">
      <button
        type="button"
        onClick={() => summary && setOpen((o) => !o)}
        aria-expanded={summary ? open : undefined}
        disabled={!summary}
        className={cn(
          "group flex w-full min-w-0 items-baseline gap-2 rounded px-1 -mx-1 py-0.5 text-left text-xs leading-5",
          summary && "cursor-pointer hover:bg-muted/40",
        )}
      >
        <Glyph className={cn("size-3 shrink-0 translate-y-0.5", mark.tone)} aria-hidden />
        <span
          className={cn(
            "min-w-0 flex-1 text-muted-foreground",
            // A report's first two lines are the report: it is the one row an
            // agent chose to send, so it gets more than a line before it opens.
            !open && (isReport ? "line-clamp-2" : "truncate"),
          )}
        >
          {name && <span className="text-foreground-dim">{name} </span>}
          {mark.label}
          {!open && summary && (
            <>
              {": "}
              <span className={cn(quoted && "italic", "text-foreground/80")}>{summary}</span>
            </>
          )}
        </span>
        {time && (
          <span className="shrink-0 font-mono text-[10px] tabular-nums text-muted-foreground-faint">
            {time}
          </span>
        )}
      </button>
      {open && summary && (
        <div className="ml-5 mt-0.5 mb-1">
          {quoted ? (
            <>
              <blockquote className="border-l-2 border-agent/40 pl-2 text-xs italic leading-5 text-foreground/80 whitespace-pre-wrap break-words">
                {summary}
              </blockquote>
              <span className="text-[10px] text-muted-foreground-faint">{attribution}</span>
            </>
          ) : (
            <p className="text-xs leading-5 text-foreground/90 whitespace-pre-wrap break-words">
              {summary}
            </p>
          )}
        </div>
      )}
    </li>
  );
});

/**
 * A run of updates behind one line. Closed by default; the count and the span
 * of clock time are what a reader needs to decide whether to open it.
 */
export const EventFold = memo(function EventFold({
  entries,
  label,
}: {
  entries: AssistantJournalEntry[];
  /** Overrides the lead-in, for a digest's "based on" fold. */
  label?: string;
}) {
  const [open, setOpen] = useState(false);
  if (entries.length === 0) return null;
  const first = clockTime(entries[0]?.at);
  const last = clockTime(entries[entries.length - 1]?.at);
  const span = first && last && first !== last ? `${first}–${last}` : first;
  const words = label ?? `${entries.length} updates`;

  return (
    <div className="min-w-0">
      <button
        type="button"
        onClick={() => setOpen((o) => !o)}
        aria-expanded={open}
        className="flex cursor-pointer items-center gap-1.5 rounded px-1 -mx-1 py-0.5 text-[11px] text-muted-foreground hover:bg-muted/40"
      >
        <ChevronRight
          className={cn(
            "size-3 transition-transform motion-reduce:transition-none",
            open && "rotate-90",
          )}
          aria-hidden
        />
        {words}
        {span && (
          <span className="font-mono text-[10px] tabular-nums text-muted-foreground-faint">
            {span}
          </span>
        )}
      </button>
      {open && (
        <ul className="mt-0.5 ml-1.5 flex flex-col border-l border-border/60 pl-3">
          {entries.map((entry, index) => (
            <EventRow key={entry.id ?? `${entry.at}-${index}`} entry={entry} />
          ))}
        </ul>
      )}
    </div>
  );
});

/** Where the reader's last look ended. Drawn once per visit and not moved. */
export function UnseenDivider() {
  return (
    <div className="flex items-center gap-2 text-[10px] font-medium uppercase tracking-wider text-primary">
      <span aria-hidden className="h-px flex-1 bg-primary/35" />
      since you last looked
      <span aria-hidden className="h-px flex-1 bg-primary/35" />
    </div>
  );
}
