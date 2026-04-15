import { useCallback, useEffect, useRef, useState } from "react";

const SpeechRecognitionCtor =
  typeof window !== "undefined"
    ? (window.SpeechRecognition ?? window.webkitSpeechRecognition)
    : undefined;

interface UseSpeechRecognitionOptions {
  /** Called with the accumulated transcript every time results update. */
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
    recognitionRef.current = null;
    try {
      rec?.stop();
    } catch {
      // InvalidStateError if recognition wasn't actually started.
      // State is already reset — nothing to do.
    }
  }, [resetState]);

  const start = useCallback(() => {
    if (!SpeechRecognitionCtor || listeningRef.current) return;

    // Abort any leftover instance (shouldn't exist, but defensive).
    recognitionRef.current?.abort();
    recognitionRef.current = null;

    const recognition = new SpeechRecognitionCtor();
    recognition.continuous = true;
    recognition.interimResults = true;
    if (lang) recognition.lang = lang;

    const mySession = ++sessionIdRef.current;

    recognition.onresult = (event: SpeechRecognitionEvent) => {
      if (sessionIdRef.current !== mySession) return;
      let transcript = "";
      for (let i = 0; i < event.results.length; i++) {
        const alt = event.results[i]?.[0];
        if (alt) transcript += alt.transcript;
      }
      onTranscriptRef.current(transcript);
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
