/**
 * The phone's call surface: one bubble above the composer, and a sheet behind
 * it.
 *
 * It exists only while a call does. An idle mic floating over the thread would
 * duplicate the composer's own Live button and cover the message it sits on;
 * what the phone actually needs is the opposite — while a call is up, hanging
 * it up must be one thumb-reach away from anywhere in the app.
 */
import { AudioLines } from "lucide-react";
import { useState } from "react";
import { Sheet, SheetContent, SheetDescription, SheetTitle } from "~/components/ui/sheet";
import { CallLog, CallStatusDot, callStatusLine } from "~/components/voice/CallLog";
import { useCallView } from "~/components/voice/use-call-view";
import { useIsMobile } from "~/hooks/useIsMobile";
import { cn } from "~/lib/utils";

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
        aria-label="Open the call"
        // Above the composer and clear of the home indicator: a control that
        // lands under the keyboard tray is a control nobody can press.
        className={cn(
          "fixed right-4 bottom-[calc(env(safe-area-inset-bottom)+5.5rem)] z-40",
          "flex size-12 items-center justify-center rounded-full shadow-lg",
          "bg-agent/15 text-agent ring-1 ring-agent/30 backdrop-blur-sm",
          view.status === "live" && "animate-pulse motion-reduce:animate-none",
          view.status === "error" && "bg-destructive/15 text-destructive ring-destructive/30",
        )}
      >
        <AudioLines className="size-5" />
      </button>

      <Sheet open={open} onOpenChange={setOpen}>
        <SheetContent side="bottom" className="max-h-[80vh] gap-0 p-0" showCloseButton={false}>
          <SheetTitle className="sr-only">Live call</SheetTitle>
          <SheetDescription className="sr-only">
            What the call has said and done, and the way to end it.
          </SheetDescription>

          <div className="flex items-center gap-2 border-b px-4 py-3">
            <CallStatusDot status={view.status} />
            <span className="min-w-0 flex-1">
              <span className="block truncate text-sm font-medium">
                {view.focusName || "No focus"}
              </span>
              <span className="block truncate text-xs text-muted-foreground">
                {callStatusLine(view.status, view.detail)}
              </span>
            </span>
          </div>

          <div className="max-h-[45vh] overflow-y-auto overscroll-contain">
            <CallLog log={view.log} status={view.status} />
          </div>

          {/* The point of the sheet. Full width, thumb height, one tap. */}
          <div className="border-t p-4 pb-[max(1rem,env(safe-area-inset-bottom))]">
            <button
              type="button"
              onClick={() => {
                view.stop();
                setOpen(false);
              }}
              className="flex h-12 w-full cursor-pointer items-center justify-center rounded-xl bg-destructive/10 text-sm font-semibold text-destructive transition-colors hover:bg-destructive/20"
            >
              End call
            </button>
          </div>
        </SheetContent>
      </Sheet>
    </>
  );
}
