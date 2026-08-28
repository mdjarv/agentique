/**
 * The call as a driver can use it: one screen, one control, three facts.
 *
 * The strip is the right shape when the phone is in your hand and the session
 * it is driving is the thing you came to watch. In a car it is the wrong shape
 * for every reason at once — a 36px control beside an 11px caption, read at
 * arm's length, in daylight, by someone who must not look for longer than a
 * glance. So hands-free is not a restyled strip. It is a different answer to a
 * different question, and it takes the whole screen because in a car there is
 * nothing else on the screen worth keeping.
 *
 * What it shows, in the order it is asked for:
 *
 * 1. **Is it hearing me** — the orb, at 132px, halo and all. It is the one
 *    fault a driver cannot check any other way: the arc moves when the
 *    microphone does, so a dead line is visible without reading a word.
 * 2. **Where will this land** — the focused session, second-largest thing on
 *    screen. It is what "send it" hits, and it is the one mistake this feature
 *    must not make.
 * 3. **What is it doing** — the same single line every other surface renders,
 *    at a size that survives a glance.
 *
 * And one control. While driving there is exactly one thing worth pressing, and
 * every additional button is a target to miss.
 *
 * Three rules the ordinary surfaces do not follow, because a windscreen is not
 * a desk:
 *
 * - **Solid fills, not tints.** The rest of the app draws an action as a 10%
 *   wash over the background. Behind glass in daylight that is a rectangle you
 *   cannot find, and this is the surface where finding it matters.
 * - **Nothing scrolls.** Text clamps and is cut off instead. Scrolling is a
 *   gesture that needs a second look at the screen; a truncated line is one the
 *   call will say out loud anyway.
 * - **Nothing moves under the thumb.** Live and ended draw their controls at
 *   the same size in the same place, so the target does not migrate when the
 *   call ends between the look and the press.
 */
import { PhoneOff, RotateCcw, X } from "lucide-react";
import { CallLineText, FocusChip } from "~/components/voice/CallStatus";
import { HaloOrb } from "~/components/voice/HaloOrb";
import type { CallView } from "~/components/voice/use-call-view";
import { cn } from "~/lib/utils";
import { callTitle } from "~/lib/voice/copy";

/**
 * Height of the one control, in pixels.
 *
 * Well past the 44px platform minimum, which is sized for a still hand and a
 * still phone. This one is pressed against a moving car by someone who is not
 * looking at it, so it is sized to be hit by the thumb landing roughly where it
 * remembers the button being.
 */
const ACTION_HEIGHT = "h-[92px]";

export function DrivingCall({ view, onExit }: { view: CallView; onExit: () => void }) {
  return (
    <div
      // Over everything, including the composer and the mobile sheet's own
      // layer: while this is up it is the only surface, which is the point.
      className={cn(
        "fixed inset-0 z-50 flex flex-col bg-background",
        "pt-[env(safe-area-inset-top)] pb-[max(1.25rem,env(safe-area-inset-bottom))]",
      )}
      // A driving surface that a screen reader announces as an unlabelled
      // region is a driving surface for one kind of driver.
      role="dialog"
      aria-modal="true"
      aria-label="Hands-free call"
    >
      <div className="flex items-center justify-between px-4 py-3">
        <span className="text-[13px] font-semibold tracking-wide text-muted-foreground uppercase">
          Hands-free
        </span>
        {/* Quiet, because leaving is not what this screen is for — but still a
            full 56px, because it is pressed in the same conditions. */}
        <button
          type="button"
          onClick={onExit}
          aria-label="Leave hands-free"
          className={cn(
            "flex size-14 cursor-pointer items-center justify-center rounded-2xl",
            "text-muted-foreground transition-colors duration-150 active:bg-foreground/10",
          )}
        >
          <X className="size-7" />
        </button>
      </div>

      {/* min-h-0 so the clamped lines below give ground before the button does:
          the control's position is the thing that must not move. */}
      <div className="flex min-h-0 flex-1 flex-col items-center justify-center gap-6 px-6 text-center">
        <HaloOrb size={132} state={view.orbState} />

        <div className="flex w-full min-w-0 flex-col items-center gap-3">
          <h2
            className={cn(
              "text-4xl font-bold tracking-tight",
              view.ended ? "text-muted-foreground" : "text-foreground-bright",
            )}
          >
            {callTitle(view.status)}
          </h2>

          {/* Where a prompt would land. Given its own line at its own size
              rather than the strip's chip, because at a glance this is the
              fact worth the second-largest type on the screen. */}
          {view.focusName ? (
            <FocusChip
              name={view.focusName}
              className="max-w-full px-3 py-1 text-xl leading-tight"
            />
          ) : (
            <span className="text-xl text-muted-foreground-faint">No session yet</span>
          )}

          <CallLineText
            line={view.line}
            spinnerClassName="size-5"
            // Two lines, clamped rather than scrolled, and brighter than every
            // other surface draws it. Muted grey on near-black is a deliberate
            // recessive treatment on a phone in the hand; through a windscreen
            // in daylight it is simply not there, and this line is the whole
            // of what the call is doing.
            //
            // The clamp is skipped on the working line, which is a flex row
            // with a spinner in it: `line-clamp` sets `display: -webkit-box`
            // and would take the row apart. That branch is one short
            // server-written label and truncates on its own.
            className={cn(
              "max-w-full text-lg leading-snug text-foreground text-wrap break-words",
              view.line.kind !== "activity" && "line-clamp-2",
            )}
          />
        </div>
      </div>

      <div className="flex gap-3 px-4">
        {view.ended ? (
          <>
            <DrivingButton
              onClick={view.restart}
              label="Call again"
              // `text-background` rather than a foreground token: success has
              // no paired one, and the page's own ground is the high-contrast
              // ink against it in both themes.
              className="bg-success text-background"
            >
              <RotateCcw className="size-7" />
              Call again
            </DrivingButton>
            <DrivingButton
              onClick={view.dismiss}
              label="Dismiss the ended call"
              className="max-w-[38%] bg-muted text-foreground"
            >
              Dismiss
            </DrivingButton>
          </>
        ) : (
          <DrivingButton
            onClick={view.stop}
            label="End call"
            className="bg-destructive text-destructive-foreground"
          >
            <PhoneOff className="size-7" />
            End call
          </DrivingButton>
        )}
      </div>
    </div>
  );
}

/**
 * One driving control: full height, solid fill, and its own words on it.
 *
 * The label is spelled as well as drawn. A glyph alone is a thing to decode,
 * and the whole point of this surface is that nothing on it needs decoding.
 */
function DrivingButton({
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
      className={cn(
        "flex flex-1 cursor-pointer items-center justify-center gap-3 rounded-3xl",
        ACTION_HEIGHT,
        "text-2xl font-bold transition-transform duration-100 active:scale-[0.98]",
        className,
      )}
    >
      {children}
    </button>
  );
}
