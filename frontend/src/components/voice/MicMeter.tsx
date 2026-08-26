/**
 * The microphone, as five segments — the audio-path check's readout.
 *
 * The call surfaces do not use this: they draw one `HaloOrb`, and one mark for
 * one thing is the rule. What is left here is `/dev/voice`, where the question
 * is narrower — is the capture path producing samples at all — and a bare meter
 * beside a bare endpoint is the right shape for it.
 *
 * This is the cheapest possible proof that the microphone is still heard:
 * it moves when you talk, and it stops when the line dies. The level itself is
 * a module-level cell written by capture (`lib/voice/level.ts`), never store
 * state — thirty updates a second through zustand would re-render every
 * component subscribed to the call for the sake of five bars.
 *
 * So nothing here re-renders either. The animation frame writes `style.opacity`
 * on segments it already holds refs to, and React is not involved between
 * mount and unmount.
 */
import { useEffect, useRef } from "react";
import { cn } from "~/lib/utils";
import { readMicLevel } from "~/lib/voice/level";

const SEGMENTS = 5;

/** Heights of the five bars, quietest first. */
const BAR_HEIGHTS = ["3px", "5px", "7px", "9px", "11px"];

/** How dim an unlit segment is — present, so the meter reads as a meter. */
const UNLIT_OPACITY = 0.16;

/**
 * Smoothing, asymmetric on purpose: rise almost immediately so a word registers
 * the moment it is spoken, fall slowly so the meter does not strobe between
 * syllables.
 */
const ATTACK = 0.55;
const RELEASE = 0.12;

/**
 * @param live Whether audio is flowing. The loop runs only while it is true —
 *   and only while the tab is visible, since a background tab's frames are
 *   throttled and nobody is looking at the result anyway.
 */
export function MicMeter({ live, className }: { live: boolean; className?: string }) {
  const barsRef = useRef<(HTMLSpanElement | null)[]>([]);

  useEffect(() => {
    const paint = (level: number) => {
      for (let i = 0; i < SEGMENTS; i++) {
        const bar = barsRef.current[i];
        if (!bar) continue;
        // Partial credit for the segment the level is inside, so a quiet voice
        // still moves something rather than sitting between two steps.
        const lit = Math.min(1, Math.max(0, level * SEGMENTS - i));
        bar.style.opacity = String(UNLIT_OPACITY + lit * (1 - UNLIT_OPACITY));
      }
    };

    if (!live) {
      paint(0);
      return;
    }

    let frame = 0;
    let shown = 0;
    let stopped = false;

    const tick = () => {
      if (stopped) return;
      const level = readMicLevel();
      shown += (level - shown) * (level > shown ? ATTACK : RELEASE);
      paint(shown);
      frame = requestAnimationFrame(tick);
    };

    const onVisibility = () => {
      cancelAnimationFrame(frame);
      if (document.visibilityState === "visible") frame = requestAnimationFrame(tick);
      else paint(0);
    };

    if (document.visibilityState === "visible") frame = requestAnimationFrame(tick);
    document.addEventListener("visibilitychange", onVisibility);
    return () => {
      stopped = true;
      cancelAnimationFrame(frame);
      document.removeEventListener("visibilitychange", onVisibility);
    };
  }, [live]);

  return (
    <span aria-hidden className={cn("flex h-3 shrink-0 items-end gap-[2px]", className)}>
      {BAR_HEIGHTS.map((height, i) => (
        <span
          key={height}
          ref={(el) => {
            barsRef.current[i] = el;
          }}
          style={{ height, opacity: UNLIT_OPACITY }}
          className="w-[2px] rounded-full bg-agent"
        />
      ))}
    </span>
  );
}
