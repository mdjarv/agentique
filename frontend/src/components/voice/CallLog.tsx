/**
 * What the call has said and done, in one scrolling column.
 *
 * Both surfaces that can show a call — the rail's popover and the phone's sheet
 * — render this, because a call log that reads differently depending on where
 * you opened it is two logs.
 */
import { Loader2, Mic } from "lucide-react";
import { useEffect, useRef } from "react";
import { cn } from "~/lib/utils";
import type { VoiceLogEntry, VoiceStatus } from "~/stores/voice-store";

export function CallStatusDot({ status }: { status: VoiceStatus }) {
  if (status === "connecting") {
    return <Loader2 className="size-3.5 shrink-0 animate-spin text-muted-foreground" />;
  }
  return (
    <span
      aria-hidden
      className={cn(
        "size-2 shrink-0 rounded-full",
        status === "live" && "animate-pulse bg-agent motion-reduce:animate-none",
        status === "error" && "bg-destructive",
        status === "idle" && "bg-muted-foreground/40",
      )}
    />
  );
}

/** One line of status, in the words the reader needs. */
export function callStatusLine(status: VoiceStatus, detail?: string): string {
  if (detail) return detail;
  switch (status) {
    case "live":
      return "Live — listening";
    case "connecting":
      return "Connecting";
    case "error":
      return "Call failed";
    default:
      return "Not connected";
  }
}

/**
 * The log, tailing itself.
 *
 * Hands-free means nobody is going to scroll, so the newest line has to be the
 * one on screen. The scroll is the container's own — a call log must never take
 * the page with it.
 */
export function CallLog({ log, status }: { log: VoiceLogEntry[]; status: VoiceStatus }) {
  const tailRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (log.length === 0) return;
    tailRef.current?.scrollIntoView({ block: "end", behavior: "smooth" });
  }, [log]);

  if (log.length === 0) {
    return (
      <p className="px-3 py-6 text-center text-[12px] text-muted-foreground">
        {status === "live"
          ? "Listening. Say what you want done — it reads the prompt back before it starts anything."
          : status === "connecting"
            ? "Connecting…"
            : "Nothing said yet."}
      </p>
    );
  }

  return (
    <ul className="flex flex-col gap-2 px-3 py-3">
      {log.map((entry) => (
        <LogLine key={entry.id} entry={entry} />
      ))}
      <div ref={tailRef} />
    </ul>
  );
}

/**
 * One line of the call.
 *
 * The sources are styled apart on purpose: a report is agent-written text about
 * repository content, and a dispatched prompt is what actually got sent.
 * Reading them as if they were all the agent talking loses the distinction that
 * matters most when something goes wrong.
 */
function LogLine({ entry }: { entry: VoiceLogEntry }) {
  if (entry.source === "you") {
    return (
      <li className="max-w-[85%] self-end rounded-lg bg-muted/60 px-2.5 py-1.5 text-[12.5px]">
        {entry.text}
      </li>
    );
  }
  if (entry.source === "agent") {
    return (
      <li className="max-w-[85%] self-start rounded-lg bg-agent/10 px-2.5 py-1.5 text-[12.5px]">
        {entry.text}
      </li>
    );
  }
  if (entry.source === "dispatched") {
    return (
      <li className="rounded-lg bg-agent/5 px-2.5 py-1.5 ring-1 ring-agent/25">
        <div className="mb-1 flex items-center gap-1.5 text-[10px] font-semibold uppercase tracking-wider text-agent">
          <Mic className="size-3" />
          Sent to the session
        </div>
        <p className="text-[12.5px]">{entry.text}</p>
      </li>
    );
  }
  return (
    <li className="rounded-lg bg-muted/30 px-2.5 py-1.5 ring-1 ring-border/60">
      <div className="mb-1 text-[10px] font-semibold uppercase tracking-wider text-muted-foreground-faint">
        {entry.source === "notice" ? (entry.kind ?? "update") : `report · ${entry.kind ?? ""}`}
      </div>
      <p className="text-[12.5px] text-muted-foreground">{entry.text}</p>
    </li>
  );
}
