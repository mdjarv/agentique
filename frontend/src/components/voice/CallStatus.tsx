/**
 * The two small parts every call surface repeats: the focus chip and the one
 * subordinate line.
 *
 * They live here rather than in each surface because the rail dock, the phone
 * strip and the sheet describe the same call, and a reader moving between them
 * should not have to translate. The liveness mark is not here — that is
 * `HaloOrb`, which is one component for the same reason.
 */
import { Loader2 } from "lucide-react";
import type { CallLine } from "~/components/voice/use-call-view";
import { cn } from "~/lib/utils";

/**
 * Which session the call is pointed at, beside the title rather than as it.
 *
 * Absent when there is no focus: a call without one is still a call, and a chip
 * saying "No focus" spends the same room saying nothing. The arrow prefix reads
 * as "aimed at", which is exactly what focus is — the one session `run_prompt`
 * can reach.
 *
 * It is capped and truncated rather than allowed to size the row. Session names
 * are user text and go on as long as the user likes.
 */
export function FocusChip({ name, className }: { name: string | null; className?: string }) {
  if (!name) return null;
  return (
    <span
      title={name}
      className={cn(
        "max-w-[46%] shrink-0 truncate rounded-full border px-1.5 py-px text-[10px] leading-[15px]",
        "border-primary/35 bg-primary/[0.13] text-primary",
        className,
      )}
    >
      {`▸ ${name}`}
    </span>
  );
}

/**
 * The one subordinate line, in the form its kind deserves.
 *
 * Interim text is styled as provisional — italic and dimmed — because it is
 * about to be rewritten, and reading a half-recognised sentence as if it were
 * settled is how a transcript misleads.
 *
 * Every branch truncates and every branch is `min-w-0`. Dictation arrives as
 * one unbroken run of words with no upper bound, so a line that can grow is a
 * line that will push a card wider than the rail it sits in.
 */
export function CallLineText({ line, className }: { line: CallLine; className?: string }) {
  if (line.kind === "activity") {
    return (
      <span
        className={cn(
          "flex min-w-0 max-w-full items-center gap-1.5 text-muted-foreground",
          className,
        )}
      >
        <Loader2 className="size-3 shrink-0 animate-spin text-info motion-reduce:animate-none" />
        <span className="min-w-0 truncate">{line.text}</span>
      </span>
    );
  }
  if (line.kind === "interim") {
    return (
      <span
        className={cn(
          "block min-w-0 max-w-full truncate italic",
          line.source === "agent" ? "text-success/75" : "text-muted-foreground/75",
          className,
        )}
      >
        {line.text}
      </span>
    );
  }
  return (
    <span className={cn("block min-w-0 max-w-full truncate text-muted-foreground", className)}>
      {line.text}
    </span>
  );
}
