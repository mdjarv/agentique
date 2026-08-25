import { Loader2, Mic, PhoneOff, X } from "lucide-react";
import { useEffect, useRef } from "react";
import { createPortal } from "react-dom";
import { useVoiceCall, type VoiceLogEntry } from "~/hooks/useVoiceCall";
import { cn } from "~/lib/utils";

/**
 * A live voice call, bound to one session.
 *
 * Full-screen rather than a popover: the primary surface is a phone in a car
 * mount, where the call *is* the screen. On desktop it still takes over, which
 * is honest — a call is a mode, not a widget.
 */
export function LiveCallPanel({
  sessionId,
  sessionName,
  onClose,
}: {
  sessionId: string;
  sessionName: string;
  onClose: () => void;
}) {
  const { state, detail, log, start, stop } = useVoiceCall(sessionId);
  const tailRef = useRef<HTMLDivElement>(null);

  // Follow the tail. Hands-free means nobody is going to scroll, so a log that
  // needs scrolling to read is a log nobody reads.
  useEffect(() => {
    if (log.length === 0) return;
    tailRef.current?.scrollIntoView({ block: "end", behavior: "smooth" });
  }, [log]);

  // Start on open: the user already pressed a button that says Live, and a
  // second "connect" tap is a step nobody wants while driving.
  useEffect(() => {
    start();
    // start is stable for a given sessionId; re-running would drop the call.
  }, [start]);

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") onClose();
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [onClose]);

  const hangUp = () => {
    stop();
    onClose();
  };

  // Portalled to <body>: `position: fixed` is relative to the nearest ancestor
  // with a transform, filter, backdrop-filter or containment, and the chat tree
  // has several. Rendered in place the panel is fixed to the composer rather
  // than the viewport, so it opens as a box halfway down the screen.
  return createPortal(
    <div className="fixed inset-0 z-50 flex flex-col bg-background/95 backdrop-blur-sm">
      <header className="flex items-center gap-3 border-b px-4 py-3">
        <StatusDot state={state} />
        <div className="min-w-0 flex-1">
          <div className="text-sm font-medium truncate">{sessionName || "Live"}</div>
          <div className="text-xs text-muted-foreground truncate">{statusLine(state, detail)}</div>
        </div>
        <button
          type="button"
          onClick={onClose}
          className="h-9 w-9 rounded-lg flex items-center justify-center text-muted-foreground hover:bg-muted/80 cursor-pointer"
          aria-label="Close"
        >
          <X className="h-4 w-4" />
        </button>
      </header>

      <div className="flex-1 overflow-y-auto px-4 py-4">
        {log.length === 0 ? (
          <p className="mx-auto max-w-md pt-10 text-center text-sm text-muted-foreground">
            {state === "live"
              ? "Listening. Say what you want done — it will read the prompt back before it starts anything."
              : "Connecting…"}
          </p>
        ) : (
          <ul className="mx-auto flex max-w-2xl flex-col gap-3">
            {log.map((entry) => (
              <LogLine key={entry.id} entry={entry} />
            ))}
            <div ref={tailRef} />
          </ul>
        )}
      </div>

      <footer className="flex items-center justify-center gap-3 border-t px-4 py-4 pb-[max(1rem,env(safe-area-inset-bottom))]">
        <button
          type="button"
          onClick={hangUp}
          className="flex h-12 items-center gap-2 rounded-full bg-destructive/10 px-6 text-sm font-medium text-destructive transition-colors hover:bg-destructive/20 cursor-pointer"
        >
          <PhoneOff className="h-4 w-4" />
          End call
        </button>
      </footer>
    </div>,
    document.body,
  );
}

function StatusDot({ state }: { state: string }) {
  if (state === "connecting") {
    return <Loader2 className="h-4 w-4 shrink-0 animate-spin text-muted-foreground" />;
  }
  return (
    <span
      className={cn(
        "h-2.5 w-2.5 shrink-0 rounded-full",
        state === "live" && "bg-agent animate-pulse",
        state === "failed" && "bg-destructive",
        (state === "idle" || state === "closed") && "bg-muted-foreground/40",
      )}
    />
  );
}

function statusLine(state: string, detail?: string): string {
  if (detail) return detail;
  switch (state) {
    case "live":
      return "Live — listening";
    case "connecting":
      return "Connecting";
    case "closed":
      return "Call ended";
    case "failed":
      return "Call failed";
    default:
      return "Not connected";
  }
}

/**
 * One line of the call.
 *
 * The four sources are styled apart on purpose: a report is agent-written text
 * about repository content, and a dispatched prompt is what actually got sent.
 * Reading them as if they were all the agent talking loses the distinction that
 * matters most when something goes wrong.
 */
function LogLine({ entry }: { entry: VoiceLogEntry }) {
  if (entry.source === "you") {
    return (
      <li className="self-end max-w-[85%] rounded-lg bg-muted/60 px-3 py-2 text-sm">
        {entry.text}
      </li>
    );
  }
  if (entry.source === "agent") {
    return (
      <li className="self-start max-w-[85%] rounded-lg bg-agent/10 px-3 py-2 text-sm">
        {entry.text}
      </li>
    );
  }
  if (entry.source === "dispatched") {
    return (
      <li className="rounded-lg border border-agent/30 bg-agent/5 px-3 py-2">
        <div className="mb-1 flex items-center gap-1.5 text-[10px] font-medium uppercase tracking-wider text-agent">
          <Mic className="h-3 w-3" />
          Sent to the session
        </div>
        <p className="text-sm">{entry.text}</p>
      </li>
    );
  }
  return (
    <li className="rounded-lg border bg-muted/30 px-3 py-2">
      <div className="mb-1 text-[10px] font-medium uppercase tracking-wider text-muted-foreground">
        {entry.source === "notice" ? (entry.kind ?? "update") : `report · ${entry.kind ?? ""}`}
      </div>
      <p className="text-sm text-muted-foreground">{entry.text}</p>
    </li>
  );
}
