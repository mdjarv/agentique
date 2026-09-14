/**
 * What the composer's status line says while dictation runs.
 *
 * One table, so the word, the sub-line and the leading mark cannot disagree
 * about which phase they describe. The browser route has no phases to report —
 * its words stream as they are heard — so it reads as listening throughout; the
 * server route has a real wait between speaking and seeing text, and each phase
 * of it gets its own words.
 */
import type { DictationRoute } from "~/hooks/useDictation";
import type { DictationPhase } from "./server-dictation";

/** The leading mark: a spinner, the live waveform, a pulse, or the writing shimmer. */
export type StatusMark = "spinner" | "wave" | "pulse" | "writing";

export interface DictationStatusCopy {
  mark: StatusMark;
  word: string;
  /** Where the audio goes and what to do next. `stopHint` places the Esc key. */
  sub: string;
  stopHint: boolean;
}

export function dictationStatus(
  route: DictationRoute,
  phase: DictationPhase | null,
): DictationStatusCopy {
  if (route === "browser") {
    return { mark: "pulse", word: "Listening", sub: "via the browser", stopHint: true };
  }
  switch (phase) {
    case "hearing":
      return { mark: "wave", word: "Hearing you", sub: "via Gemini", stopHint: true };
    case "writing":
      return { mark: "writing", word: "Writing it down", sub: "via Gemini", stopHint: false };
    case "listening":
      return {
        mark: "wave",
        word: "Listening",
        sub: "text appears when you pause",
        stopHint: true,
      };
    default:
      return {
        mark: "spinner",
        word: "Connecting",
        sub: "opening the mic · Gemini",
        stopHint: false,
      };
  }
}
