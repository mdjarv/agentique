import { useCallback, useEffect, useRef, useState } from "react";
import { reduceTranscript } from "~/lib/speech-transcript";

const SpeechRecognitionCtor =
  typeof window !== "undefined"
    ? (window.SpeechRecognition ?? window.webkitSpeechRecognition)
    : undefined;

interface UseSpeechRecognitionOptions {
  /**
   * Called with the transcript so far every time results update. Whitespace is
   * normalized (single spaces between parts, none at either end), so callers can
   * concatenate it onto their own text without guarding against doubled spaces.
   */
  onTranscript: (transcript: string) => void;
  /** Called once before recognition starts — use to snapshot pre-speech state. */
  onBeforeStart?: () => void;
  /** Called when recognition stops (user toggle, error, or silence timeout). */
  onEnd?: () => void;
  /** BCP-47 language tag. Defaults to browser locale. */
  lang?: string;
}

export function useSpeechRecognition({
  onTranscript,
  onBeforeStart,
  onEnd,
  lang,
}: UseSpeechRecognitionOptions) {
  const [isListening, setIsListening] = useState(false);
  const recognitionRef = useRef<SpeechRecognition | null>(null);

  // Synchronous mirror of isListening — immune to React batching.
  const listeningRef = useRef(false);

  // Monotonic session counter. Each start() increments this.
  // Handlers check whether they belong to the current session —
  // stale callbacks from a previous (aborted) instance are ignored.
  const sessionIdRef = useRef(0);

  const onTranscriptRef = useRef(onTranscript);
  onTranscriptRef.current = onTranscript;
  const onBeforeStartRef = useRef(onBeforeStart);
  onBeforeStartRef.current = onBeforeStart;
  const onEndRef = useRef(onEnd);
  onEndRef.current = onEnd;

  const isSupported = !!SpeechRecognitionCtor;

  // Reset all state to "not listening". Shared by stop paths.
  const resetState = useCallback(() => {
    listeningRef.current = false;
    setIsListening(false);
  }, []);

  const forceStop = useCallback(() => {
    // Unconditional teardown — the escape hatch.
    const rec = recognitionRef.current;
    if (rec) {
      rec.onresult = null;
      rec.onerror = null;
      rec.onend = null;
      rec.abort();
    }
    recognitionRef.current = null;
    sessionIdRef.current++;
    resetState();
  }, [resetState]);

  const stop = useCallback(() => {
    if (!listeningRef.current) return;
    resetState();
    const rec = recognitionRef.current;

    // The ref keeps pointing at `rec` until its own onend clears it. stop() is
    // cooperative — that is the whole reason it isn't abort(): the engine still
    // flushes one last onresult and only then releases the microphone. Dropping
    // the ref here would leave a quick re-click's start() with nothing to abort,
    // so the old recognizer would hold the device while the new one takes it.
    // The session counter deliberately does not move: that final flush belongs
    // to this session and the stale-callback guard must still let it through.
    try {
      rec?.stop();
    } catch {
      // InvalidStateError — recognition never actually started, so it holds no
      // device and will emit no onend. Drop it rather than leave a dud in the ref.
      recognitionRef.current = null;
    }
  }, [resetState]);

  const start = useCallback(() => {
    if (!SpeechRecognitionCtor || listeningRef.current) return;

    // Reclaim the microphone from a leftover instance: after stop() the previous
    // recognizer stays in the ref until its onend, so a fast re-click lands here
    // while it is still winding down. Its handlers stay attached — the session
    // bump below is what silences them.
    recognitionRef.current?.abort();
    recognitionRef.current = null;

    const recognition = new SpeechRecognitionCtor();
    recognition.continuous = true;
    recognition.interimResults = true;
    if (lang) recognition.lang = lang;

    const mySession = ++sessionIdRef.current;

    recognition.onresult = (event: SpeechRecognitionEvent) => {
      if (sessionIdRef.current !== mySession) return;
      // The list is authoritative and complete on every event, so the transcript
      // is derived from it rather than accumulated here. See reduceTranscript
      // for why interim results must not be added to it.
      onTranscriptRef.current(reduceTranscript(event.results));
    };

    recognition.onend = () => {
      if (sessionIdRef.current !== mySession) return;
      recognitionRef.current = null;
      resetState();
      onEndRef.current?.();
    };

    recognition.onerror = (event: SpeechRecognitionErrorEvent) => {
      if (event.error === "aborted") return;
      console.warn("[speech]", event.error, event.message);
      if (event.error === "no-speech") return;
      if (sessionIdRef.current !== mySession) return;
      stop();
    };

    onBeforeStartRef.current?.();
    recognitionRef.current = recognition;
    listeningRef.current = true;
    setIsListening(true);

    try {
      recognition.start();
    } catch (err) {
      // NotAllowedError (permission denied) or InvalidStateError.
      console.warn("[speech] failed to start:", err);
      // Detach handlers so the failed instance can't fire stale events.
      recognition.onresult = null;
      recognition.onerror = null;
      recognition.onend = null;
      recognitionRef.current = null;
      resetState();
    }
  }, [lang, stop, resetState]);

  const toggle = useCallback(() => {
    if (listeningRef.current) {
      stop();
    } else {
      start();
    }
  }, [start, stop]);

  useEffect(() => {
    return () => {
      // Clean teardown — detach handlers so abort doesn't trigger stale setState.
      const rec = recognitionRef.current;
      if (rec) {
        rec.onresult = null;
        rec.onerror = null;
        rec.onend = null;
        rec.abort();
      }
    };
  }, []);

  return { isListening, isSupported, start, stop, forceStop, toggle };
}
