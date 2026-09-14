import { useEffect, useRef } from "react";
import type { DictationRoute } from "~/hooks/useDictation";
import { dictationStatus } from "~/lib/speech/dictation-status";
import type { DictationPhase } from "~/lib/speech/server-dictation";
import { cn } from "~/lib/utils";
import { readMicLevel } from "~/lib/voice/level";

interface DictationStatusProps {
  route: DictationRoute;
  phase: DictationPhase | null;
}

/**
 * The line that takes the toolbar's place while dictation runs.
 *
 * Nobody reads the model picker mid-sentence, and dictation has more to say
 * than a red mic can: whether it is still connecting, whether it hears you,
 * whether the words are on their way, and where the audio is going. So for the
 * length of a dictation the toolbar steps aside for one line, and comes back
 * when it stops. Desktop only — the phone's composer carries no mic.
 */
export function DictationStatus({ route, phase }: DictationStatusProps) {
  const copy = dictationStatus(route, phase);
  return (
    <div className="flex items-center gap-2.5 min-w-0 pl-1.5" role="status" aria-live="polite">
      <span
        className={cn(
          "flex w-7 shrink-0 justify-center",
          copy.mark === "writing"
            ? "text-agent"
            : copy.mark === "spinner"
              ? "text-muted-foreground"
              : "text-destructive",
        )}
        aria-hidden
      >
        {copy.mark === "wave" && <Waveform quiet={phase === "listening"} />}
        {copy.mark === "pulse" && <span className="h-2 w-2 rounded-full bg-current mic-pulse" />}
        {copy.mark === "writing" && <span className="dictation-writing h-2 w-6 rounded-sm" />}
        {copy.mark === "spinner" && (
          <span className="h-3 w-3 rounded-full border-[1.5px] border-current/25 border-t-current animate-spin motion-reduce:animate-none" />
        )}
      </span>
      <span className="text-[13px] font-medium text-foreground-bright whitespace-nowrap">
        {copy.word}
      </span>
      <span className="min-w-0 truncate font-mono text-[11.5px] text-muted-foreground">
        {copy.sub}
        {copy.stopHint && (
          <>
            {" · "}
            <kbd className="rounded border border-b-2 border-border px-1 font-mono text-[10.5px]">
              Esc
            </kbd>{" "}
            to stop
          </>
        )}
      </span>
    </div>
  );
}

const BARS = 7;

/**
 * The microphone's level as seven bars, drawn on its own animation frame.
 *
 * It writes heights straight to the DOM: the level arrives thirty times a
 * second and lives outside React on purpose (`lib/voice/level.ts`), so
 * rendering it through state would re-render the composer for a meter.
 */
function Waveform({ quiet }: { quiet: boolean }) {
  const ref = useRef<HTMLSpanElement>(null);

  useEffect(() => {
    const el = ref.current;
    if (!el) return;
    const reduced = window.matchMedia("(prefers-reduced-motion: reduce)").matches;
    let raf = 0;
    const draw = () => {
      const level = readMicLevel();
      const bars = el.children;
      for (let i = 0; i < bars.length; i++) {
        const bar = bars[i] as HTMLElement;
        const shape = 0.45 + 0.55 * Math.sin((i / (BARS - 1)) * Math.PI);
        const jitter = reduced ? 1 : 0.6 + Math.random() * 0.4;
        bar.style.height = `${(3 + level * shape * 11 * jitter).toFixed(1)}px`;
      }
      raf = requestAnimationFrame(draw);
    };
    raf = requestAnimationFrame(draw);
    return () => cancelAnimationFrame(raf);
  }, []);

  return (
    <span ref={ref} className={cn("flex h-3.5 items-center gap-0.5", quiet && "opacity-60")}>
      {Array.from({ length: BARS }, (_, i) => (
        // biome-ignore lint/suspicious/noArrayIndexKey: fixed set of bars
        <i key={i} className="block w-0.5 h-[3px] rounded-[1px] bg-current" />
      ))}
    </span>
  );
}
