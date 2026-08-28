/**
 * The phone's call surface: a caption strip above the composer, and a sheet
 * behind it.
 *
 * It replaced a floating bubble, and the reason is what the phone is for. The
 * call is spoken; the screen is where you watch the session it is driving. A
 * bubble covered that screen with a circle that could only say "a call
 * exists" — everything the call was actually *doing* (the words being
 * recognised, the summary being computed, the session it is pointed at) was
 * behind a tap. So the surface is a strip: full width, one line of caption,
 * docked above the composer where a caption belongs, and never over the
 * transcript.
 *
 * It exists only while a call does — or has just ended and not been dismissed.
 * An idle strip would duplicate the composer's own Live button.
 *
 * Tapping it opens the log; the log is a room, not a line, and it belongs in a
 * sheet. The sheet closes itself when the call focuses somewhere else, because
 * the point of focusing is to put that session on screen and a sheet is in
 * front of the screen.
 *
 * It mirrors the rail dock's states exactly, because it is the same call.
 *
 * Hands-free is the one thing it does not render: `DrivingCall` takes the whole
 * screen instead. The switch is in the sheet, because that is where the call's
 * own controls live, and it is remembered (`ui-store.handsFree`) so it is armed
 * once rather than at every call — the gesture is not one to make at the wheel.
 */
import { Car, ChevronsRight, PhoneOff, RotateCcw, X } from "lucide-react";
import { useEffect, useState } from "react";
import { Sheet, SheetContent, SheetDescription, SheetTitle } from "~/components/ui/sheet";
import { CallLog } from "~/components/voice/CallLog";
import { CallLineText, FocusChip } from "~/components/voice/CallStatus";
import { DrivingCall } from "~/components/voice/DrivingCall";
import { HaloOrb } from "~/components/voice/HaloOrb";
import { useCallView } from "~/components/voice/use-call-view";
import { useIsMobile } from "~/hooks/useIsMobile";
import { cn } from "~/lib/utils";
import { callTitle } from "~/lib/voice/copy";
import { useUIStore } from "~/stores/ui-store";
import { useVoiceStore } from "~/stores/voice-store";

/**
 * Above the composer and clear of the home indicator. A control that lands
 * under the keyboard tray is a control nobody can press.
 */
const DOCK_BOTTOM = "bottom-[calc(env(safe-area-inset-bottom)+5.5rem)]";

export function VoiceStrip() {
  const isMobile = useIsMobile();
  const view = useCallView();
  const focusSeq = useVoiceStore((s) => s.focusSeq);
  const handsFree = useUIStore((s) => s.handsFree);
  const setHandsFree = useUIStore((s) => s.setHandsFree);
  const [open, setOpen] = useState(false);
  const [collapsed, setCollapsed] = useState(false);

  // The screen follows the voice: a focus change is an instruction to look at
  // that session, and the sheet is what would be in the way. Watching the
  // counter rather than the id, because focusing the same session twice is two
  // instructions.
  useEffect(() => {
    // Nothing has been focused yet on a call that has never turned.
    if (focusSeq === 0) return;
    setOpen(false);
  }, [focusSeq]);

  // Tucking away is a decision about *this* call. The component stays mounted
  // between calls, so without this the next one would open already hidden.
  const active = view.active;
  useEffect(() => {
    if (!active) setCollapsed(false);
  }, [active]);

  if (!isMobile || !active) return null;

  // Hands-free replaces this surface rather than decorating it — see
  // DrivingCall for why it is a different screen and not a bigger strip. The
  // sheet and the tuck-away go with it: both are ways of getting the call out
  // of the way of something else, and in a car there is nothing else.
  if (handsFree) {
    return <DrivingCall view={view} onExit={() => setHandsFree(false)} />;
  }

  if (collapsed) {
    return (
      <button
        type="button"
        onClick={() => setCollapsed(false)}
        aria-label="Show the call"
        className={cn(
          "fixed right-4 z-40 flex size-[46px] items-center justify-center",
          DOCK_BOTTOM,
          "rounded-full border border-border bg-popover shadow-lg",
        )}
      >
        <HaloOrb size={34} state={view.orbState} />
      </button>
    );
  }

  return (
    <>
      <div
        className={cn(
          "fixed inset-x-2 z-40 flex items-center gap-1 py-2 pr-1.5 pl-2.5",
          DOCK_BOTTOM,
          "rounded-[11px] border border-border bg-popover shadow-[0_4px_16px_rgba(0,0,0,0.35)]",
        )}
      >
        {/* The caption *is* the tap target — everything but the controls, which
            stay real buttons rather than click handlers inside one. */}
        <button
          type="button"
          onClick={() => setOpen(true)}
          aria-label={view.ended ? "Show the ended call" : "Open the call"}
          className="flex min-w-0 flex-1 items-center gap-2.5 text-left"
        >
          <HaloOrb size={26} state={view.orbState} />
          {/* min-w-0 the whole way down: dictation is an unbounded run of words
              and must never push the strip wider than the screen. */}
          <span className="flex min-w-0 flex-1 flex-col gap-px">
            <span className="flex min-w-0 items-center gap-1.5">
              <span
                className={cn(
                  "min-w-0 truncate text-[11.5px] font-semibold",
                  view.ended ? "text-muted-foreground" : "text-foreground-bright",
                )}
              >
                {callTitle(view.status)}
              </span>
              <FocusChip name={view.focusName} />
            </span>
            {/* A fixed height, so the strip does not jump every time the
                caption changes kind. */}
            <span className="flex min-h-[15px] min-w-0 items-center">
              <CallLineText line={view.line} className="text-[11px]" />
            </span>
          </span>
        </button>

        {view.ended ? (
          <>
            <StripButton
              onClick={view.restart}
              label="Call again"
              className="text-muted-foreground"
            >
              <RotateCcw className="size-4" />
            </StripButton>
            <StripButton
              onClick={view.dismiss}
              label="Dismiss the ended call"
              className="text-muted-foreground-faint"
            >
              <X className="size-4" />
            </StripButton>
          </>
        ) : (
          <>
            <StripButton
              onClick={() => setCollapsed(true)}
              label="Tuck the call away"
              className="text-muted-foreground-faint"
            >
              <ChevronsRight className="size-4" />
            </StripButton>
            <StripButton onClick={view.stop} label="End call" className="text-destructive">
              <PhoneOff className="size-4" />
            </StripButton>
          </>
        )}
      </div>

      <Sheet open={open} onOpenChange={setOpen}>
        <SheetContent side="bottom" className="max-h-[78vh] gap-0 p-0" showCloseButton={false}>
          <SheetTitle className="sr-only">Live call</SheetTitle>
          <SheetDescription className="sr-only">
            What the call has said and done, and the way to end it.
          </SheetDescription>

          <div className="flex justify-center pt-2 pb-1">
            <span aria-hidden className="h-1 w-9 rounded-full bg-muted-foreground/40" />
          </div>

          <div className="flex items-center gap-2.5 border-b px-4 py-3">
            <HaloOrb size={28} state={view.orbState} />
            <span className="flex min-w-0 flex-1 items-center gap-1.5">
              <span
                className={cn(
                  "min-w-0 truncate text-sm font-semibold",
                  view.ended && "text-muted-foreground",
                )}
              >
                {callTitle(view.status)}
              </span>
              <FocusChip name={view.focusName} />
            </span>
            <StripButton
              onClick={() => setOpen(false)}
              label="Back to the strip"
              className="text-muted-foreground"
            >
              <X className="size-4" />
            </StripButton>
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
                  onClick={view.restart}
                  className="bg-success/10 text-success hover:bg-success/20"
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
              // Two rows, and hands-free is the upper one: it is a change of
              // surface, never a way of ending the call, and a driver reaching
              // for the bottom control must find the same one every time.
              <div className="flex flex-col gap-2">
                <SheetButton
                  onClick={() => {
                    setHandsFree(true);
                    setOpen(false);
                  }}
                  className="bg-muted/70 text-foreground hover:bg-muted"
                >
                  <Car className="size-4" />
                  Hands-free
                </SheetButton>
                <SheetButton
                  onClick={view.stop}
                  className="border border-destructive/40 bg-destructive/10 text-destructive hover:bg-destructive/20"
                >
                  <PhoneOff className="size-4" />
                  End call
                </SheetButton>
              </div>
            )}
          </div>
        </SheetContent>
      </Sheet>
    </>
  );
}

/** One of the strip's own controls: thumb-sized, and never the tap that opens the log. */
function StripButton({
  onClick,
  label,
  className,
  children,
}: {
  onClick: () => void;
  label: string;
  className?: string;
  children: React.ReactNode;
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      aria-label={label}
      title={label}
      className={cn(
        "flex size-9 shrink-0 cursor-pointer items-center justify-center rounded-lg",
        "transition-colors duration-150 active:bg-foreground/10",
        className,
      )}
    >
      {children}
    </button>
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
