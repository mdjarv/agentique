/**
 * What the call has said and done, in one scrolling column.
 *
 * Both surfaces that can show a call — the rail's popover and the phone's sheet
 * — render this, because a call log that reads differently depending on where
 * you opened it is two logs.
 */
import { FileText, Mic } from "lucide-react";
import { useEffect, useRef } from "react";
import { useChatStore } from "~/stores/chat-store";
import type { VoiceLogEntry, VoiceStatus } from "~/stores/voice-store";

/** What an empty log says, which depends on why it is empty. */
function emptyLine(status: VoiceStatus): string {
  switch (status) {
    case "live":
      return "Listening. Say what you want done — it reads the prompt back before it starts anything.";
    case "connecting":
      return "Connecting…";
    case "ended":
      return "Nothing was said on that call.";
    default:
      return "Nothing said yet.";
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
      <p className="px-3 py-6 text-center text-[12px] leading-relaxed text-muted-foreground">
        {emptyLine(status)}
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
      <li className="max-w-[85%] self-end rounded-lg rounded-br-sm bg-muted/70 px-2.5 py-1.5 text-[12.5px] leading-snug">
        {entry.text}
      </li>
    );
  }
  if (entry.source === "agent") {
    return (
      <li className="max-w-[85%] self-start rounded-lg rounded-bl-sm bg-agent/10 px-2.5 py-1.5 text-[12.5px] leading-snug">
        {entry.text}
      </li>
    );
  }
  if (entry.source === "summary") {
    return <SummaryCard entry={entry} />;
  }
  if (entry.source === "dispatched") {
    return (
      <li className="rounded-lg bg-agent/5 px-2.5 py-1.5 ring-1 ring-agent/25">
        <div className="mb-1 flex items-center gap-1.5 text-[10px] font-semibold uppercase tracking-wider text-agent">
          <Mic className="size-3" />
          Sent to the session
        </div>
        <p className="text-[12.5px] leading-snug">{entry.text}</p>
      </li>
    );
  }
  return (
    <li className="rounded-lg bg-muted/30 px-2.5 py-1.5 ring-1 ring-border/60">
      <div className="mb-1 text-[10px] font-semibold uppercase tracking-wider text-muted-foreground-faint">
        {entry.source === "notice" ? (entry.kind ?? "update") : `report · ${entry.kind ?? ""}`}
      </div>
      <p className="text-[12.5px] leading-snug text-muted-foreground">{entry.text}</p>
    </li>
  );
}

/**
 * A summary, as content rather than a status chip.
 *
 * It is the answer the operator asked a question to get, and it is the one
 * thing on a call that can be several sentences long, so it gets the room — and
 * its own scroll, so a long one cannot push the rest of the call off screen.
 *
 * It is agent-written text distilled from an untrusted transcript. On screen
 * that means it is displayed as content and never as an instruction: no
 * markdown rendering, no links, just the words as they arrived.
 */
function SummaryCard({ entry }: { entry: VoiceLogEntry }) {
  const sessionName = useChatStore((s) =>
    entry.sessionId ? (s.sessions[entry.sessionId]?.meta.name ?? null) : null,
  );
  return (
    <li className="rounded-lg bg-muted/45 px-3 py-2 ring-1 ring-border/70">
      <div className="mb-1.5 flex items-center gap-1.5 text-[10px] font-semibold uppercase tracking-wider text-muted-foreground">
        <FileText className="size-3 shrink-0" />
        <span className="truncate">Summary{sessionName ? ` · ${sessionName}` : ""}</span>
      </div>
      <p className="max-h-56 overflow-y-auto overscroll-contain whitespace-pre-wrap text-[12.5px] leading-relaxed">
        {entry.text}
      </p>
    </li>
  );
}
