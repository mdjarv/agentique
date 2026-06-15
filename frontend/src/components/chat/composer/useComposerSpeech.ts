import { useCallback, useEffect, useRef } from "react";
import { useSpeechRecognition } from "~/hooks/useSpeechRecognition";

interface UseComposerSpeechParams {
  /** Reads the current composer text (synchronous, ref-backed). */
  getText: () => string;
  /** Replaces the composer text. Autosize is handled by the consumer's value-keyed effect. */
  setText: (text: string) => void;
}

export interface ComposerSpeech {
  isSupported: boolean;
  isListening: boolean;
  /** Unconditional teardown — used by send before clearing. */
  forceStop: () => void;
  /** Click/keyboard toggle. */
  toggle: () => void;
  /** Spread onto the mic button. */
  micHandlers: {
    onPointerDown: (e: React.PointerEvent) => void;
    onPointerUp: () => void;
    onPointerCancel: () => void;
    onLostPointerCapture: () => void;
    onContextMenu: (e: React.MouseEvent) => void;
  };
}

const HOLD_THRESHOLD_MS = 500;

/**
 * Dictation handling for the composer:
 * - press-and-hold (>500ms) starts "hold mode", release stops;
 * - short click (<500ms) toggles;
 * - if already listening on pointer down, force-stop immediately (escape hatch).
 *
 * `speechBaseRef` snapshots the text that existed when recognition started; every
 * transcript update replaces everything after that base.
 */
export function useComposerSpeech({ getText, setText }: UseComposerSpeechParams): ComposerSpeech {
  const speechBaseRef = useRef("");

  const speech = useSpeechRecognition({
    onBeforeStart: useCallback(() => {
      speechBaseRef.current = getText();
    }, [getText]),
    onTranscript: useCallback(
      (transcript: string) => {
        const base = speechBaseRef.current;
        const spacer = base && !base.endsWith(" ") && !base.endsWith("\n") ? " " : "";
        setText(base + spacer + transcript);
      },
      [setText],
    ),
  });

  const holdTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const holdActiveRef = useRef(false);
  const didForceStopRef = useRef(false);

  const clearHoldTimer = useCallback(() => {
    if (holdTimerRef.current) {
      clearTimeout(holdTimerRef.current);
      holdTimerRef.current = null;
    }
  }, []);

  const onPointerDown = useCallback(
    (e: React.PointerEvent) => {
      if (e.button !== 0) return;
      try {
        // currentTarget is the <button>, not the lucide icon child that fires e.target —
        // capturing on the child means pointerup/cancel never reach us and the mic sticks "on".
        e.currentTarget.setPointerCapture(e.pointerId);
      } catch {
        // Pointer capture not supported or invalid pointer — continue without it.
      }

      // Clear any leftover hold timer (e.g. if a previous pointerup was missed).
      clearHoldTimer();
      didForceStopRef.current = false;

      // Escape hatch: if the UI shows listening, force-stop unconditionally.
      if (speech.isListening) {
        speech.forceStop();
        didForceStopRef.current = true;
        return;
      }

      holdActiveRef.current = false;
      holdTimerRef.current = setTimeout(() => {
        holdTimerRef.current = null;
        holdActiveRef.current = true;
        speech.start();
      }, HOLD_THRESHOLD_MS);
    },
    [speech, clearHoldTimer],
  );

  const onPointerUp = useCallback(() => {
    clearHoldTimer();
    if (didForceStopRef.current) {
      didForceStopRef.current = false;
      return;
    }
    if (holdActiveRef.current) {
      holdActiveRef.current = false;
      speech.stop();
    } else {
      speech.toggle();
    }
  }, [speech, clearHoldTimer]);

  // Pointer cancel / lost capture: the interaction was interrupted (not a clean
  // release), so tear down without toggling — this is what keeps the mic from
  // getting stuck "on" when the OS or browser steals the pointer mid-hold.
  const onPointerInterrupted = useCallback(() => {
    clearHoldTimer();
    didForceStopRef.current = false;
    if (holdActiveRef.current) {
      holdActiveRef.current = false;
      speech.stop();
    }
  }, [speech, clearHoldTimer]);

  // Clean up hold timer if the component unmounts mid-press.
  useEffect(() => clearHoldTimer, [clearHoldTimer]);

  return {
    isSupported: speech.isSupported,
    isListening: speech.isListening,
    forceStop: speech.forceStop,
    toggle: speech.toggle,
    micHandlers: {
      onPointerDown,
      onPointerUp,
      onPointerCancel: onPointerInterrupted,
      onLostPointerCapture: onPointerInterrupted,
      onContextMenu: (e: React.MouseEvent) => e.preventDefault(),
    },
  };
}
