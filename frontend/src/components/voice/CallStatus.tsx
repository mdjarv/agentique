/**
 * The call's status vocabulary: one dot, one subordinate line.
 *
 * Both live here rather than in each surface, because the rail dock and the
 * phone sheet describe the same call and a reader moving between them should
 * not have to translate.
 */
import { Loader2 } from "lucide-react";
import type { CallLine } from "~/components/voice/use-call-view";
import { cn } from "~/lib/utils";
import type { VoiceStatus } from "~/stores/voice-store";

/**
 * Where the call is, as one mark.
 *
 * Connecting is the only animated state: it is the only one that is going to
 * change on its own. Live used to pulse, which said nothing — the meter next
 * to it now says the same thing with real data, and a decorative pulse that
 * keeps going after the line has died is worse than no pulse at all.
 */
export function CallStatusDot({ status }: { status: VoiceStatus }) {
  return (
    <span aria-hidden className="relative flex size-2 shrink-0 items-center justify-center">
      {status === "connecting" && (
        <span className="absolute inline-flex size-2 animate-ping rounded-full bg-agent/60 motion-reduce:hidden" />
      )}
      <span
        className={cn(
          "size-2 rounded-full transition-colors duration-200",
          status === "live" && "bg-agent",
          status === "connecting" && "bg-agent/70",
          status === "error" && "bg-destructive",
          status === "ended" && "bg-muted-foreground/50",
          status === "idle" && "bg-muted-foreground/40",
        )}
      />
    </span>
  );
}

/**
 * The one subordinate line, in the form its kind deserves.
 *
 * Interim text is styled as provisional — italic and dimmed — because it is
 * about to be rewritten, and reading a half-recognised sentence as if it were
 * settled is how a transcript misleads.
 */
export function CallLineText({ line, className }: { line: CallLine; className?: string }) {
  if (line.kind === "activity") {
    return (
      <span className={cn("flex min-w-0 items-center gap-1.5 text-muted-foreground", className)}>
        <Loader2 className="size-3 shrink-0 animate-spin" />
        <span className="truncate">{line.text}</span>
      </span>
    );
  }
  if (line.kind === "interim") {
    return (
      <span
        className={cn(
          "block truncate italic",
          line.source === "agent" ? "text-agent/75" : "text-muted-foreground/75",
          className,
        )}
      >
        {line.text}
      </span>
    );
  }
  return <span className={cn("block truncate text-muted-foreground", className)}>{line.text}</span>;
}
