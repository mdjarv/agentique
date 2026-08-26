/**
 * The call, as one mark — at every size it is ever shown.
 *
 * A call appears in four places (the rail's idle row, the rail's card, the
 * phone's strip, the phone's sheet) and each used to draw its own liveness cue.
 * They drifted: a ring here, five bars there, a dot that said "live" while the
 * meter said silence. So there is one orb, and the only thing that changes
 * between surfaces is how many pixels it gets.
 *
 * The halo is the meter. A green arc grows clockwise from twelve o'clock with
 * the microphone level, over a faint full-circle track that says how much arc
 * there could be. The core is the state in a glyph: a **phone** while there is
 * no line open — idle, dialling, hung up — and a **microphone** once there is.
 * You place a call; then the microphone is open. Two glyphs, one transition,
 * and it happens exactly when the thing it describes happens.
 *
 * The level is read here, on this component's own animation frame, from the
 * module-level cell capture writes (`lib/voice/level.ts`). It never travels
 * through zustand: thirty store updates a second would re-render every
 * component subscribed to the call, for an arc. Nothing here re-renders between
 * mount and unmount either — the frame writes `stroke-dashoffset` on a node it
 * already holds.
 */
import { Mic, Phone } from "lucide-react";
import { useEffect, useRef } from "react";
import { cn } from "~/lib/utils";
import { readMicLevel } from "~/lib/voice/level";

/**
 * What the orb is describing.
 *
 * `working` is `live` with the call busy on something of its own — same arc,
 * same glyph, and it exists so a surface can say "still live, and doing
 * something" without inventing a second vocabulary.
 */
export type HaloState = "idle" | "connecting" | "live" | "working" | "ended";

/** Geometry in viewBox units; the whole thing scales with `size`. */
const R = 44;
const STROKE = 8;
const CIRC = 2 * Math.PI * R;

/** The chase arc while dialling: a quarter turn, going round. */
const CHASE_FRACTION = 0.25;

/**
 * Silence still draws a little arc.
 *
 * The level is data and is not scaled or floored anywhere else, but an arc that
 * disappears completely between words reads as a dead line rather than a quiet
 * one — which is the opposite of what this mark is for.
 */
const LIVE_FLOOR = 0.06;

/** Rise fast so a word registers, fall slow so it does not strobe. */
const ATTACK = 0.55;
const RELEASE = 0.12;

function offsetFor(fraction: number): number {
  return CIRC * (1 - Math.min(1, Math.max(0, fraction)));
}

/**
 * @param size Diameter in pixels. Everything else is proportional to it.
 * @param arcClassName Hook for the parent to drive the arc itself — the idle
 *   row uses it to draw the halo to full on hover. It works because the resting
 *   offset is an SVG *attribute*, which any CSS rule outranks; the live arc
 *   writes an inline style instead, which outranks both.
 */
export function HaloOrb({
  size,
  state,
  className,
  arcClassName,
}: {
  size: number;
  state: HaloState;
  className?: string;
  arcClassName?: string;
}) {
  const arcRef = useRef<SVGCircleElement>(null);
  const metering = state === "live" || state === "working";

  useEffect(() => {
    const arc = arcRef.current;
    if (!arc) return;
    if (!metering) {
      // Hand the arc back to CSS: an inline offset left over from the last call
      // would pin it open, and the hover draw would have nothing to move.
      arc.style.strokeDashoffset = "";
      return;
    }

    let frame = 0;
    let shown = 0;
    let stopped = false;

    const paint = (level: number) => {
      arc.style.strokeDashoffset = String(offsetFor(LIVE_FLOOR + level * (1 - LIVE_FLOOR)));
    };

    const tick = () => {
      if (stopped) return;
      const level = readMicLevel();
      shown += (level - shown) * (level > shown ? ATTACK : RELEASE);
      paint(shown);
      frame = requestAnimationFrame(tick);
    };

    // A background tab throttles frames and nobody is looking at the result.
    const onVisibility = () => {
      cancelAnimationFrame(frame);
      if (document.visibilityState === "visible") frame = requestAnimationFrame(tick);
      else paint(0);
    };

    paint(0);
    if (document.visibilityState === "visible") frame = requestAnimationFrame(tick);
    document.addEventListener("visibilitychange", onVisibility);
    return () => {
      stopped = true;
      cancelAnimationFrame(frame);
      document.removeEventListener("visibilitychange", onVisibility);
    };
  }, [metering]);

  const chasing = state === "connecting";
  const Glyph = metering ? Mic : Phone;

  return (
    <span
      aria-hidden
      data-halo-state={state}
      className={cn("relative inline-flex shrink-0 items-center justify-center", className)}
      style={{ width: size, height: size }}
    >
      <svg viewBox="0 0 100 100" className="absolute inset-0 size-full">
        <title>Call</title>
        {/* The track: how much arc there could be, so an empty halo still reads
            as a meter rather than as nothing. */}
        <circle
          cx="50"
          cy="50"
          r={R}
          fill="none"
          stroke="var(--border)"
          strokeWidth={STROKE}
          opacity={0.6}
        />
        {/* Rotated so the arc grows from twelve o'clock. The spin lives on the
            group because a CSS transform on the arc would replace that rotate. */}
        <g className={cn(chasing && "origin-center animate-spin motion-reduce:animate-none")}>
          <circle
            ref={arcRef}
            cx="50"
            cy="50"
            r={R}
            fill="none"
            stroke={state === "ended" ? "var(--muted-foreground)" : "var(--success)"}
            strokeWidth={STROKE}
            strokeLinecap="round"
            strokeDasharray={CIRC}
            strokeDashoffset={offsetFor(chasing ? CHASE_FRACTION : 0)}
            transform="rotate(-90 50 50)"
            className={cn(
              // Only while the arc is not being driven per frame: a 450ms
              // transition under a 60Hz writer is a lagging arc.
              !metering && "transition-[stroke-dashoffset] duration-[450ms] ease-out",
              arcClassName,
            )}
          />
        </g>
        {/* The core sits inside the halo, in the surface colour, so the glyph
            reads against a card as well as against the rail. */}
        <circle cx="50" cy="50" r={R - STROKE} fill="var(--card)" />
      </svg>
      <Glyph
        className={cn(
          "relative",
          state === "ended"
            ? "text-muted-foreground"
            : metering
              ? "text-success"
              : "text-foreground",
        )}
        style={{ width: size * 0.36, height: size * 0.36 }}
      />
    </span>
  );
}
