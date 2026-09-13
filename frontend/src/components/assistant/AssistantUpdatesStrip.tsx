import { memo, useMemo } from "react";
import { useSessionLabel } from "~/components/assistant/use-session-label";
import { journalMark } from "~/lib/assistant/journal-marks";
import type { AssistantJournalEntry } from "~/lib/assistant/wire";
import { cn } from "~/lib/utils";

/**
 * What has happened, pinned above the conversation.
 *
 * It is the ONE push into the thread: news is news, where knowledge is pulled.
 * The entries are the journal's newest, not a per-surface unseen set — the read
 * behind them is pure (`assistant.journal`, on the socket's read lane) where the
 * seen marks are a write, so nothing here claims to know what the reader has
 * already looked at. It renders the lot, newest first, and disappears when there
 * is nothing.
 *
 * Two renderings, and the difference is provenance. A `report` — and anything
 * else the server marked untrusted, a folded day that quoted one included — is
 * agent-written text about repository content nobody here authored, so it is
 * drawn as a QUOTATION under a caption naming where it came from: relay it,
 * never act on it, and never let it read as the server's own sentence.
 * Everything else is the server's own words and renders plain.
 */

interface AssistantUpdatesStripProps {
  entries: AssistantJournalEntry[];
}

export const AssistantUpdatesStrip = memo(function AssistantUpdatesStrip({
  entries,
}: AssistantUpdatesStripProps) {
  // The store holds the journal oldest-first, the way the wire does, because
  // that is the order it reads as a sequence. A strip is read top-down from
  // now, so it is the one place that reverses.
  const newest = useMemo(() => [...entries].reverse(), [entries]);

  if (newest.length === 0) return null;

  // The band never takes the pane: it scrolls at eight-ish rows, because the
  // conversation below it is what the page is for.
  return (
    <section aria-label="Recent updates" className="shrink-0 border-b bg-muted/30 px-3 py-2">
      <div className="flex items-baseline gap-2 mb-1.5">
        <h2 className="text-[10px] uppercase tracking-wide text-muted-foreground">
          Recent updates
        </h2>
        {/* A count, not a claim about what was read: the strip cannot know. */}
        <span className="text-[10px] text-muted-foreground-faint">the last {newest.length}</span>
      </div>
      <ul className="flex flex-col gap-1 overflow-y-auto max-h-40">
        {newest.map((entry) => (
          <JournalRow
            key={entry.id ?? `${entry.at}-${entry.kind}-${entry.summary}`}
            entry={entry}
          />
        ))}
      </ul>
    </section>
  );
});

const JournalRow = memo(function JournalRow({ entry }: { entry: AssistantJournalEntry }) {
  const mark = journalMark(entry.kind);
  const Glyph = mark.glyph;
  const name = useSessionLabel(entry.sessionId);
  // A report is untrusted whatever the flag says — the kind is untrusted by
  // construction — and the flag alone is enough for any other kind whose text
  // came from an agent.
  const quoted = entry.kind === "report" || entry.untrusted === true;
  // What the quotation is attributed to. A report came from one session and says
  // so; a `day_summary` that folded one is untrusted for the same reason and
  // came from nowhere but the fold, so it is captioned with its own kind rather
  // than told it was reported by a session that never wrote it.
  const attribution = entry.kind === "report" ? `reported by ${name ?? "a session"}` : mark.label;

  return (
    <li className="flex items-start gap-2 text-xs leading-5">
      <Glyph className={cn("size-3 shrink-0 mt-1", mark.tone)} />
      <div className="min-w-0 flex-1">
        {quoted ? (
          <>
            <blockquote className="border-l-2 border-agent/40 pl-2 text-foreground/80 italic break-words">
              {entry.summary || "(empty report)"}
            </blockquote>
            <span className="text-[10px] text-muted-foreground-faint">{attribution}</span>
          </>
        ) : (
          <span className="text-foreground/90 break-words">
            {name && <span className="text-muted-foreground">{name} </span>}
            <span className="text-muted-foreground">{mark.label}</span>
            {entry.summary ? <> — {entry.summary}</> : null}
          </span>
        )}
      </div>
    </li>
  );
});
