/**
 * The assistant's row in the rail — the one way in.
 *
 * It holds the slot the Live row held, on the argument that placed that row:
 * always-true state that is the operator's, not the machine's. The assistant is
 * that state; the call was the first thing that fit. So the row names the
 * assistant, and its mark is the orb with nothing in its core: the orb is the
 * assistant's face, and a call is that same face awake, arc running,
 * microphone showing — `VoiceDock` draws the card when one exists.
 *
 * Two labels in the space of one, as the Live row had: "Assistant" at rest,
 * and under the pointer the verb the click performs. `⌥A` does the same from
 * anywhere with a keyboard; `⌥V` still places a call, because the two are
 * different asks and the second must not cost a screen.
 *
 * **Memory's flare rides the orb's track.** When memory changes — anywhere,
 * any tab — the track pulses once. It used to pulse the rail's ⋯ trigger, which
 * is no longer where memory lives; the row is, because the orb is the
 * assistant's face and memory is the assistant's. It is a pulse and not a
 * notch: nothing is owed a look, the point is only that the thing is alive.
 *
 * **The notch is the only mark it may wear.** The rail indicates with marks,
 * not sentences: a second line that changed by itself here is what the footer
 * argued off the rail at 271px. The notch means what it means on a session
 * chip — something arrived that you have not looked at — and it goes when the
 * thread is on screen. The number is spoken to a screen reader and drawn for
 * nobody.
 */
import { useNavigate } from "@tanstack/react-router";
import { useCallback, useEffect, useRef, useState } from "react";
import { LabelSwap } from "~/components/layout/LabelSwap";
import { HaloOrb } from "~/components/voice/HaloOrb";
import { dismissSidebar } from "~/lib/sidebar-nav";
import { cn } from "~/lib/utils";
import { selectAssistantUnseen, useAssistantStore } from "~/stores/assistant-store";
import { useBrainStore } from "~/stores/brain-store";

export function AssistantRow({ showShortcut }: { showShortcut: boolean }) {
  const navigate = useNavigate();
  const unseen = useAssistantStore(selectAssistantUnseen);
  const flaring = useMemoryFlare();

  const open = useCallback(() => {
    // Arriving dismisses the phone's drawer through the router rule; a click
    // on the page you are already on never navigates, so this says it too.
    dismissSidebar();
    void navigate({ to: "/assistant" });
  }, [navigate]);
  useAssistantShortcut(showShortcut, open);

  const label =
    unseen > 0
      ? `Ask the assistant, ${unseen} unseen ${unseen === 1 ? "update" : "updates"}`
      : "Ask the assistant";

  return (
    <button
      type="button"
      onClick={open}
      aria-label={label}
      title={showShortcut ? "Ask the assistant (⌥A)" : "Ask the assistant"}
      className={cn(
        "group flex w-full cursor-pointer items-center gap-2 rounded-md px-1.5 py-1 text-left",
        "text-[11.5px] text-muted-foreground transition-colors duration-150",
        "max-md:py-2 hover:bg-sidebar-accent/60",
      )}
    >
      <span className="relative shrink-0">
        <HaloOrb
          size={24}
          state="idle"
          glyph="none"
          // The arc is an attribute at rest, so this rule wins and the halo
          // draws itself round under the pointer.
          arcClassName="group-hover:[stroke-dashoffset:0]"
          trackClassName={flaring ? "orb-track-flare" : undefined}
        />
        {unseen > 0 && (
          <span
            aria-hidden
            data-testid="assistant-unseen"
            className="absolute -top-px -right-px size-[7px] rounded-full bg-primary ring-2 ring-sidebar"
          />
        )}
      </span>
      <LabelSwap resting="Assistant" hovered="Ask the assistant" />
      {showShortcut && (
        <kbd
          className={cn(
            "ml-auto shrink-0 rounded border border-border px-1 py-px font-mono text-[9.5px]",
            "text-muted-foreground-faint opacity-0 transition-opacity duration-150",
            "group-hover:opacity-100",
          )}
        >
          ⌥A
        </kbd>
      )}
    </button>
  );
}

/**
 * True for one pulse after memory changes (a fact added, edited or removed, or
 * a consolidation applied — here or in another tab).
 *
 * The brain store's `flareSeq` bumps on every `brain.updated` push, so this
 * watches the number rather than the events. The initial value is skipped: a
 * row mounting is not news.
 */
function useMemoryFlare(): boolean {
  const flareSeq = useBrainStore((s) => s.flareSeq);
  const [flaring, setFlaring] = useState(false);
  const seenRef = useRef(flareSeq);
  useEffect(() => {
    if (flareSeq === seenRef.current) return;
    seenRef.current = flareSeq;
    setFlaring(true);
    // A touch longer than the keyframe, so the class outlives the animation
    // rather than cutting it off mid-pulse.
    const t = setTimeout(() => setFlaring(false), 1300);
    return () => clearTimeout(t);
  }, [flareSeq]);
  return flaring;
}

/**
 * `⌥A` from anywhere, doing exactly what the row does.
 *
 * The same guard as the call's `⌥V`: a keystroke inside a composer, a rename
 * field or any editable surface belongs to whatever the operator is writing,
 * and an IME composition is not a keystroke at all yet.
 */
function useAssistantShortcut(enabled: boolean, open: () => void): void {
  useEffect(() => {
    if (!enabled) return;
    const onKeyDown = (e: KeyboardEvent) => {
      if (!e.altKey || e.ctrlKey || e.metaKey || e.isComposing) return;
      // On macOS Alt+A yields "å", so the physical key is the reliable test.
      if (e.code !== "KeyA" && e.key.toLowerCase() !== "a") return;
      const target = e.target as HTMLElement | null;
      const tag = target?.tagName;
      if (tag === "INPUT" || tag === "TEXTAREA" || target?.isContentEditable) return;
      e.preventDefault();
      open();
    };
    window.addEventListener("keydown", onKeyDown);
    return () => window.removeEventListener("keydown", onKeyDown);
  }, [enabled, open]);
}
