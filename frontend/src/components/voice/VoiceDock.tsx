/**
 * The rail's voice band — the desktop half of the app-wide call.
 *
 * A call is no longer a mode that owns the screen: it navigates, so it cannot
 * cover the thing it navigates to. What is left is one line at the bottom of
 * the sidebar, next to the other things that are always true — quiet when there
 * is no call, and never more than a line plus a popover when there is.
 *
 * The phone gets `VoiceBubble` instead: the rail is behind a sheet there, and a
 * hangup you have to open a drawer to reach is not a hangup.
 */
import { AudioLines, PhoneOff } from "lucide-react";
import { useState } from "react";
import { Popover, PopoverContent, PopoverTrigger } from "~/components/ui/popover";
import { CallLog, CallStatusDot, callStatusLine } from "~/components/voice/CallLog";
import { useCallView } from "~/components/voice/use-call-view";
import { useIsMobile } from "~/hooks/useIsMobile";
import { cn } from "~/lib/utils";
import { useChatStore } from "~/stores/chat-store";
import { useFeatureStore } from "~/stores/feature-store";
import { useVoiceStore } from "~/stores/voice-store";

export function VoiceDock() {
  const isMobile = useIsMobile();
  const voiceEnabled = useFeatureStore((s) => s.features.voice);
  const view = useCallView();
  const [open, setOpen] = useState(false);

  // On a phone the rail lives inside a sheet; the bubble is the call's surface
  // there, and two of them would compete.
  if (isMobile) return null;
  // A call already running outlives the flag being read: never strand one
  // without a way to hang it up.
  if (!voiceEnabled && !view.active) return null;

  return (
    <div className="shrink-0 border-t border-sidebar-border px-2 py-1.5">
      {view.active ? (
        <Popover open={open} onOpenChange={setOpen}>
          <div className="flex items-center gap-1">
            <PopoverTrigger asChild>
              <button
                type="button"
                aria-label="Show the call"
                className="flex min-w-0 flex-1 cursor-pointer items-center gap-2 rounded-md px-1.5 py-1 text-left transition-colors hover:bg-sidebar-accent/60"
              >
                <CallStatusDot status={view.status} />
                <span className="min-w-0 flex-1">
                  <span className="block truncate text-[11.5px] font-medium">
                    {view.focusName || "No focus"}
                  </span>
                  <span className="block truncate text-[10.5px] text-muted-foreground">
                    {view.lastSpoken || callStatusLine(view.status, view.detail)}
                  </span>
                </span>
              </button>
            </PopoverTrigger>
            <EndCallButton onEnd={view.stop} />
          </div>

          {/* Above the bar, and scrolling inside itself — the page never moves
              because a call said something. */}
          <PopoverContent side="top" align="start" className="w-80 p-0">
            <div className="flex items-center gap-2 border-b px-3 py-2">
              <CallStatusDot status={view.status} />
              <span className="min-w-0 flex-1 truncate text-[12px] font-medium">
                {view.focusName || "No focus"}
              </span>
              <span className="shrink-0 text-[10.5px] text-muted-foreground">
                {callStatusLine(view.status, view.detail)}
              </span>
            </div>
            <div className="max-h-[min(60vh,26rem)] overflow-y-auto overscroll-contain">
              <CallLog log={view.log} status={view.status} />
            </div>
          </PopoverContent>
        </Popover>
      ) : (
        <StartCallButton />
      )}
    </div>
  );
}

/**
 * The way in. It starts on whatever session is open, because that is what the
 * operator is looking at when they reach for it; from the deck it starts with
 * no focus and the call finds its own.
 */
function StartCallButton() {
  const start = useVoiceStore((s) => s.start);
  const activeSessionId = useChatStore((s) => s.activeSessionId);
  return (
    <button
      type="button"
      onClick={() => start(activeSessionId ?? undefined)}
      className="flex w-full cursor-pointer items-center gap-2 rounded-md px-1.5 py-1 text-left text-[11.5px] text-muted-foreground transition-colors hover:bg-sidebar-accent/60 hover:text-foreground"
    >
      <AudioLines className="size-3.5 shrink-0" />
      Voice
    </button>
  );
}

function EndCallButton({ onEnd, className }: { onEnd: () => void; className?: string }) {
  return (
    <button
      type="button"
      onClick={onEnd}
      aria-label="End call"
      title="End call"
      className={cn(
        "flex size-7 shrink-0 cursor-pointer items-center justify-center rounded-md text-destructive transition-colors hover:bg-destructive/15",
        className,
      )}
    >
      <PhoneOff className="size-3.5" />
    </button>
  );
}
