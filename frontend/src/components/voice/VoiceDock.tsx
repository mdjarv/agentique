/**
 * The rail's voice band — the desktop half of the app-wide call.
 *
 * A call is not a mode that owns the screen: it navigates, so it cannot cover
 * the thing it navigates to. What is left at the bottom of the sidebar has two
 * forms, and the difference between them is the point.
 *
 * **No call: a trace.** One quiet row, the same weight as everything else that
 * is always true down here. Hovering it draws the halo round and rolls the
 * label over to what the click would actually do — the affordance is the
 * animation, so nothing has to shout while nobody is reaching for it. `⌥V` does
 * the same thing without the reach, and the chip appears on hover to say so.
 *
 * **A call: a card.** Raised out of the rail on its own surface, because a live
 * call is not one more navigation row — it is the one thing here that is
 * happening. The orb anchors it, the title says what it is, the chip says where
 * it is pointed, and the line underneath says what it is doing right now.
 *
 * A call that ends stays as an ended card until it is dismissed. A dock that
 * vanishes on hangup takes the call's answers with it, and the first field
 * report of this feature was a call that ended by itself with a summary still
 * owed.
 *
 * The phone gets `VoiceStrip` instead: the rail is behind a sheet there, and a
 * hangup you have to open a drawer to reach is not a hangup.
 */
import { PhoneOff, RotateCcw, X } from "lucide-react";
import { useEffect, useState } from "react";
import { Popover, PopoverContent, PopoverTrigger } from "~/components/ui/popover";
import { CallLog } from "~/components/voice/CallLog";
import { CallLineText, FocusChip } from "~/components/voice/CallStatus";
import { HaloOrb } from "~/components/voice/HaloOrb";
import { useCallView } from "~/components/voice/use-call-view";
import { useIsMobile } from "~/hooks/useIsMobile";
import { cn } from "~/lib/utils";
import { callStatusLine, callTitle } from "~/lib/voice/copy";
import { useChatStore } from "~/stores/chat-store";
import { useFeatureStore } from "~/stores/feature-store";
import { useVoiceStore } from "~/stores/voice-store";

export function VoiceDock() {
  const isMobile = useIsMobile();
  const voiceEnabled = useFeatureStore((s) => s.features.voice);
  const view = useCallView();
  const [open, setOpen] = useState(false);

  // The shortcut is mounted from the entry it duplicates, so it exists exactly
  // when the entry does — and hooks run before the early returns below.
  useLiveCallShortcut(voiceEnabled && !isMobile);

  // On a phone the rail lives inside a sheet; the strip is the call's surface
  // there, and two of them would compete.
  if (isMobile) return null;
  // A call already running outlives the flag being read: never strand one
  // without a way to hang it up.
  if (!voiceEnabled && !view.active) return null;

  return (
    <div className="shrink-0 border-t border-sidebar-border px-2 py-1.5">
      {view.active ? (
        <Popover open={open} onOpenChange={setOpen}>
          <div
            className={cn(
              "flex items-center gap-1.5 rounded-[9px] border border-border bg-popover px-1.5 py-1.5",
              "shadow-[0_2px_10px_rgba(0,0,0,0.28)]",
            )}
          >
            <PopoverTrigger asChild>
              <button
                type="button"
                aria-label="Show the call"
                className={cn(
                  "flex min-w-0 flex-1 cursor-pointer items-center gap-2 rounded-md px-1 py-0.5 text-left",
                  "transition-colors duration-150 hover:bg-foreground/[0.04]",
                )}
              >
                <HaloOrb size={30} state={view.orbState} />
                {/* min-w-0 the whole way down, or a long dictation line makes
                    the card wider than the rail it lives in. */}
                <span className="flex min-w-0 flex-1 flex-col gap-px">
                  <span className="flex min-w-0 items-center gap-1.5">
                    <span
                      className={cn(
                        "min-w-0 truncate text-[13px] font-semibold",
                        view.ended ? "text-muted-foreground" : "text-foreground-bright",
                      )}
                    >
                      {callTitle(view.status)}
                    </span>
                    <FocusChip name={view.focusName} />
                  </span>
                  <CallLineText line={view.line} className="text-[11.5px]" />
                </span>
              </button>
            </PopoverTrigger>
            {view.ended ? (
              <>
                <IconButton
                  onClick={view.restart}
                  label="Call again"
                  className="text-muted-foreground hover:bg-foreground/[0.06] hover:text-foreground"
                >
                  <RotateCcw className="size-3.5" />
                </IconButton>
                <IconButton
                  onClick={view.dismiss}
                  label="Dismiss the ended call"
                  className="text-muted-foreground-faint hover:bg-foreground/[0.06] hover:text-foreground"
                >
                  <X className="size-3.5" />
                </IconButton>
              </>
            ) : (
              <IconButton
                onClick={view.stop}
                label="End call"
                className="text-destructive hover:bg-destructive/15"
              >
                <PhoneOff className="size-3.5" />
              </IconButton>
            )}
          </div>

          {/* Above the bar, and scrolling inside itself — the page never moves
              because a call said something. */}
          <PopoverContent side="top" align="start" className="w-80 p-0">
            <div className="flex items-center gap-2 border-b px-3 py-2">
              <HaloOrb size={26} state={view.orbState} />
              <span className="flex min-w-0 flex-1 items-center gap-1.5">
                <span className="min-w-0 truncate text-[12px] font-semibold">
                  {callTitle(view.status)}
                </span>
                <FocusChip name={view.focusName} />
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
        <StartCallRow />
      )}
    </div>
  );
}

/**
 * The way in, at rest.
 *
 * It starts on whatever session is open, because that is what the operator is
 * looking at when they reach for it; from the deck it starts with no focus and
 * the call finds its own.
 *
 * The hover is doing the work a second line of copy would otherwise do: the
 * halo fills, "Live" rolls up to "Start live call", and the shortcut appears.
 * At rest it is four characters and a grey ring, which is all a row that is
 * true all the time should cost.
 */
function StartCallRow() {
  const start = useVoiceStore((s) => s.start);
  const activeSessionId = useChatStore((s) => s.activeSessionId);
  return (
    <button
      type="button"
      onClick={() => start(activeSessionId ?? undefined)}
      aria-label="Start live call"
      title="Start live call (⌥V)"
      className={cn(
        "group flex w-full cursor-pointer items-center gap-2 rounded-md px-1.5 py-1 text-left",
        "text-[11.5px] text-muted-foreground transition-colors duration-150",
        "hover:bg-sidebar-accent/60",
      )}
    >
      <HaloOrb
        size={24}
        state="idle"
        // The arc is an attribute at rest, so this rule wins and the halo draws
        // itself round under the pointer.
        arcClassName="group-hover:[stroke-dashoffset:0]"
      />
      <LabelSwap resting="Live" hovered="Start live call" />
      <kbd
        className={cn(
          "ml-auto shrink-0 rounded border border-border px-1 py-px font-mono text-[9.5px]",
          "text-muted-foreground-faint opacity-0 transition-opacity duration-150",
          "group-hover:opacity-100",
        )}
      >
        ⌥V
      </kbd>
    </button>
  );
}

/**
 * Two labels in the space of one, the second rolling up over the first.
 *
 * A tooltip would say the same thing later and elsewhere; this says it in
 * place, at the moment the pointer arrives. With reduced motion it is still two
 * labels and still swaps — it just does not travel. The button carries the
 * spoken name, so both halves are decoration to a screen reader.
 */
function LabelSwap({ resting, hovered }: { resting: string; hovered: string }) {
  return (
    <span aria-hidden className="block h-4 min-w-0 overflow-hidden">
      <span
        className={cn(
          "flex flex-col transition-transform duration-[220ms] ease-out",
          "group-hover:-translate-y-4 motion-reduce:transition-none",
        )}
      >
        <span className="block h-4 truncate leading-4">{resting}</span>
        <span className="block h-4 truncate leading-4 text-success">{hovered}</span>
      </span>
    </span>
  );
}

/**
 * `⌥V` from anywhere, doing exactly what the row does.
 *
 * Typing is not a shortcut: a keystroke inside a composer, a rename field or
 * any editable surface belongs to whatever the operator is writing, and an IME
 * composition is not a keystroke at all yet.
 */
function useLiveCallShortcut(enabled: boolean): void {
  const start = useVoiceStore((s) => s.start);
  const activeSessionId = useChatStore((s) => s.activeSessionId);

  useEffect(() => {
    if (!enabled) return;
    const onKeyDown = (e: KeyboardEvent) => {
      if (!e.altKey || e.ctrlKey || e.metaKey || e.isComposing) return;
      // On macOS Alt+V yields "√", so the physical key is the reliable test.
      if (e.code !== "KeyV" && e.key.toLowerCase() !== "v") return;
      const target = e.target as HTMLElement | null;
      const tag = target?.tagName;
      if (tag === "INPUT" || tag === "TEXTAREA" || target?.isContentEditable) return;
      e.preventDefault();
      // `start` is a no-op on a call already up, so this cannot open a second.
      start(activeSessionId ?? undefined);
    };
    window.addEventListener("keydown", onKeyDown);
    return () => window.removeEventListener("keydown", onKeyDown);
  }, [enabled, start, activeSessionId]);
}

function IconButton({
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
        "flex size-7 shrink-0 cursor-pointer items-center justify-center rounded-md",
        "transition-colors duration-150",
        className,
      )}
    >
      {children}
    </button>
  );
}
