/**
 * The phone's call surface: one bubble above the composer, and a sheet behind
 * it.
 *
 * It exists only while a call does — or has just ended and not been dismissed.
 * An idle mic floating over the thread would duplicate the composer's own Live
 * button and cover the message it sits on; what the phone actually needs is the
 * opposite — while a call is up, hanging it up must be one thumb-reach away
 * from anywhere in the app, and when it ends, saying so must not require
 * noticing that something disappeared.
 *
 * It mirrors the rail dock's states exactly, because it is the same call.
 */
import { AudioLines, PhoneOff, RotateCcw } from "lucide-react";
import { useState } from "react";
import { Sheet, SheetContent, SheetDescription, SheetTitle } from "~/components/ui/sheet";
import { CallLog } from "~/components/voice/CallLog";
import { CallLineText, CallStatusDot } from "~/components/voice/CallStatus";
import { MicMeter } from "~/components/voice/MicMeter";
import { useCallView } from "~/components/voice/use-call-view";
import { useIsMobile } from "~/hooks/useIsMobile";
import { cn } from "~/lib/utils";
import { callHeadline } from "~/lib/voice/copy";

export function VoiceBubble() {
  const isMobile = useIsMobile();
  const view = useCallView();
  const [open, setOpen] = useState(false);

  if (!isMobile || !view.active) return null;

  return (
    <>
      <button
        type="button"
        onClick={() => setOpen(true)}
        aria-label={view.ended ? "Show the ended call" : "Open the call"}
        // Above the composer and clear of the home indicator: a control that
        // lands under the keyboard tray is a control nobody can press.
        className={cn(
          "fixed right-4 bottom-[calc(env(safe-area-inset-bottom)+5.5rem)] z-40",
          // `fixed` is its own positioning context, which is what the ring
          // meter's `absolute inset-0` hangs off.
          "flex size-12 items-center justify-center rounded-full shadow-lg",
          "transition-colors duration-200 backdrop-blur-sm",
          "bg-agent/15 text-agent ring-1 ring-agent/30",
          view.status === "connecting" && "animate-pulse motion-reduce:animate-none",
          view.status === "error" && "bg-destructive/15 text-destructive ring-destructive/30",
          view.ended && "bg-muted/70 text-muted-foreground ring-border",
        )}
      >
        {/* The ring is the meter: on a 48px circle a five-bar meter is a
            smudge, but a ring that swells with the voice is legible at arm's
            length. It is driven by the same level, in the same loop. */}
        {!view.ended && <MicMeter live={view.live} variant="ring" />}
        {view.ended ? <PhoneOff className="size-5" /> : <AudioLines className="size-5" />}
      </button>

      <Sheet open={open} onOpenChange={setOpen}>
        <SheetContent side="bottom" className="max-h-[80vh] gap-0 p-0" showCloseButton={false}>
          <SheetTitle className="sr-only">Live call</SheetTitle>
          <SheetDescription className="sr-only">
            What the call has said and done, and the way to end it.
          </SheetDescription>

          <div className="flex items-center gap-2.5 border-b px-4 py-3">
            <CallStatusDot status={view.status} />
            <span className="min-w-0 flex-1">
              <span
                className={cn(
                  "block truncate text-sm font-medium",
                  view.ended && "text-muted-foreground",
                )}
              >
                {callHeadline(view.status, view.focusName)}
              </span>
              <CallLineText line={view.line} className="text-xs" />
            </span>
            <MicMeter live={view.live} />
          </div>

          <div className="max-h-[45vh] overflow-y-auto overscroll-contain">
            <CallLog log={view.log} status={view.status} />
          </div>

          {/* The point of the sheet. Full width, thumb height, one tap. */}
          <div className="border-t p-4 pb-[max(1rem,env(safe-area-inset-bottom))]">
            {view.ended ? (
              // Hanging up leaves the call here, ended, so the log — and the
              // summary in it — survives the gesture that ended it. Dismiss is
              // the second tap, never the same one.
              <div className="flex gap-2">
                <SheetButton
                  onClick={() => {
                    view.restart();
                  }}
                  className="bg-agent/10 text-agent hover:bg-agent/20"
                >
                  <RotateCcw className="size-4" />
                  Call again
                </SheetButton>
                <SheetButton
                  onClick={() => {
                    view.dismiss();
                    setOpen(false);
                  }}
                  className="bg-muted/70 text-muted-foreground hover:bg-muted"
                >
                  Dismiss
                </SheetButton>
              </div>
            ) : (
              <SheetButton
                onClick={view.stop}
                className="bg-destructive/10 text-destructive hover:bg-destructive/20"
              >
                <PhoneOff className="size-4" />
                End call
              </SheetButton>
            )}
          </div>
        </SheetContent>
      </Sheet>
    </>
  );
}

function SheetButton({
  onClick,
  className,
  children,
}: {
  onClick: () => void;
  className?: string;
  children: React.ReactNode;
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      className={cn(
        "flex h-12 flex-1 cursor-pointer items-center justify-center gap-2 rounded-xl",
        "text-sm font-semibold transition-colors duration-150",
        className,
      )}
    >
      {children}
    </button>
  );
}
