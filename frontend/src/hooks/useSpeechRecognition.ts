/**
 * Dictation, as one span the operator started — not as the recognition sessions
 * the browser chooses to slice it into.
 *
 * **What the browsers actually do.** `continuous = true` is a request, not a
 * guarantee. Android Chrome ignores it outright: recognition ends at the first
 * pause, every time, so a dictated sentence is five or six sessions. Desktop
 * Chrome honours it for a while and then ends anyway — its speech service
 * restarts every few seconds of continuous speech, and a long enough silence
 * ends the session too. In every one of those cases the browser fires `onend`
 * mid-sentence while the operator is still talking.
 *
 * The hook used to report that as the end of dictation. The mic went idle
 * mid-sentence, the operator pressed it again, and each press re-ran the
 * caller's `onBeforeStart` — which snapshots the composer text *including
 * everything dictated so far* — while the fresh session re-offered the audio
 * the old one had not committed. Every press therefore re-appended words that
 * were already on screen, and on a phone that is a press per breath: "I I I
 * tried I tried I tried opening…".
 *
 * So the span is the unit here. `start()` opens one and snapshots the base
 * exactly once; `onend` continues it with a new recognizer and no new snapshot;
 * `stop()`, a fatal error, or a run of silent sessions closes it. Exactly one
 * recognizer is ever live, and only *finals* cross the seam between sessions —
 * an interim is a guess the engine never committed, and the audio behind it is
 * what the next session re-recognizes. Dropping it there is what keeps
 * re-delivered words from landing twice.
 */
import { useCallback, useEffect, useRef, useState } from "react";
import { joinTranscript, reduceTranscriptParts } from "~/lib/speech-transcript";

const SpeechRecognitionCtor =
  typeof window !== "undefined"
    ? (window.SpeechRecognition ?? window.webkitSpeechRecognition)
    : undefined;

/**
 * How many recognition sessions in a row may end without producing a single
 * result before the span gives up.
 *
 * This is the silence timeout, and it is also the loop guard: a browser that
 * ends every session immediately (revoked permission the error code does not
 * name, a speech service that will not come up) is answered by three tries
 * rather than a restart loop. Any result at all refills the budget, so it only
 * ever counts silence.
 */
const SILENT_SESSION_LIMIT = 3;

/**
 * Errors no restart gets past. Everything else — `network` above all, which
 * Android throws routinely and transiently — is left to the budget above.
 */
const FATAL_ERRORS = new Set([
  "not-allowed",
  "service-not-allowed",
  "audio-capture",
  "language-not-supported",
  "bad-grammar",
]);

interface UseSpeechRecognitionOptions {
  /**
   * Called with the transcript so far every time results update. Whitespace is
   * normalized (single spaces between parts, none at either end), so callers can
   * concatenate it onto their own text without guarding against doubled spaces.
   *
   * The transcript spans the whole dictation, not the current recognition
   * session, so a caller replaces everything after its base with this — it never
   * appends to what it was handed last.
   */
  onTranscript: (transcript: string) => void;
  /**
   * Called once before a dictation span starts — use to snapshot pre-speech
   * state. Deliberately *not* called when the browser ends a session mid-span
   * and the hook continues it: re-snapshotting there is what duplicated text.
   */
  onBeforeStart?: () => void;
  /** Called when the span ends (user toggle, fatal error, or silence timeout). */
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

  // Synchronous mirror of isListening — immune to React batching. True for the
  // whole span, including the gap between one session ending and the next
  // starting: the operator is still dictating, so the mic must not blink.
  const listeningRef = useRef(false);

  // Monotonic session counter. Each recognizer created increments this.
  // Handlers check whether they belong to the current session —
  // stale callbacks from a previous (aborted) instance are ignored.
  const sessionIdRef = useRef(0);

  // The span's committed text: finals handed over by sessions that have already
  // ended. The live session's own finals are held apart until its onend, so a
  // session that ends without committing anything adds nothing.
  const committedRef = useRef("");
  const sessionFinalsRef = useRef("");
  const silentSessionsRef = useRef(0);

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

  /**
   * The span is over: the mic goes idle and the caller hears about it once.
   * Reached from a session's onend (guarded by the session counter, so once) or
   * from a recognizer that refused to start (which then fires no onend).
   */
  const endSpan = useCallback(() => {
    resetState();
    onEndRef.current?.();
  }, [resetState]);

  const forceStop = useCallback(() => {
    // Unconditional teardown — the escape hatch.
    discard(recognitionRef.current);
    recognitionRef.current = null;
    sessionIdRef.current++;
    committedRef.current = "";
    sessionFinalsRef.current = "";
    resetState();
  }, [resetState]);

  const stop = useCallback(() => {
    if (!listeningRef.current) return;
    // Flipping this first is also what tells the pending onend that the span is
    // over rather than one more slice of it: the restart path reads this flag.
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

  // beginSession restarts itself through this ref: a new recognizer is created
  // from the old one's onend, and a callback cannot name itself.
  const beginSessionRef = useRef<() => void>(() => {});

  /**
   * One recognition session inside the current span. Called by `start()` for the
   * first, and by the previous session's `onend` for every one after it — the
   * difference between those two is exactly the base snapshot, which is why
   * nothing in here touches it.
   */
  const beginSession = useCallback(() => {
    if (!SpeechRecognitionCtor) return;

    const recognition = new SpeechRecognitionCtor();
    recognition.continuous = true;
    recognition.interimResults = true;
    if (lang) recognition.lang = lang;

    const mySession = ++sessionIdRef.current;
    sessionFinalsRef.current = "";
    let sawResult = false;

    recognition.onresult = (event: SpeechRecognitionEvent) => {
      if (sessionIdRef.current !== mySession) return;
      sawResult = true;
      // The list is authoritative and complete on every event, so the transcript
      // is derived from it rather than accumulated here. See reduceTranscript
      // for why interim results must not be added to it.
      const { finals, interim } = reduceTranscriptParts(event.results);
      sessionFinalsRef.current = finals;
      onTranscriptRef.current(
        joinTranscript(committedRef.current, joinTranscript(finals, interim)),
      );
    };

    recognition.onend = () => {
      if (sessionIdRef.current !== mySession) return;
      recognitionRef.current = null;

      // Only the committed half crosses the seam. A dangling interim is a guess
      // the engine is about to re-offer to the next session, so keeping it would
      // print those words twice.
      committedRef.current = joinTranscript(committedRef.current, sessionFinalsRef.current);
      sessionFinalsRef.current = "";
      silentSessionsRef.current = sawResult ? 0 : silentSessionsRef.current + 1;

      // The operator asked for this one: stop() flips the flag before the engine
      // winds down, so the span really is over.
      if (!listeningRef.current) {
        endSpan();
        return;
      }
      if (silentSessionsRef.current >= SILENT_SESSION_LIMIT) {
        endSpan();
        return;
      }

      // Same span, next session. Publishing the committed text here is what
      // takes the dropped guess back off screen, so the words the next session
      // re-recognizes replace it instead of following it.
      onTranscriptRef.current(committedRef.current);
      beginSessionRef.current();
    };

    recognition.onerror = (event: SpeechRecognitionErrorEvent) => {
      if (sessionIdRef.current !== mySession) return;
      if (event.error === "aborted") return;
      // Silence is not a failure. onend follows immediately, and the span either
      // continues or runs out of its budget there — one rule, one place.
      if (event.error === "no-speech") return;
      console.warn("[speech]", event.error, event.message);
      if (!FATAL_ERRORS.has(event.error)) return;
      stop();
    };

    recognitionRef.current = recognition;

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
      endSpan();
    }
  }, [lang, stop, endSpan]);
  beginSessionRef.current = beginSession;

  const start = useCallback(() => {
    if (!SpeechRecognitionCtor || listeningRef.current) return;

    // Reclaim the microphone from a leftover instance: after stop() the previous
    // recognizer stays in the ref until its onend, so a fast re-click lands here
    // while it is still winding down. It belongs to the span that just ended, so
    // it is discarded outright rather than left to speak into this one.
    discard(recognitionRef.current);
    recognitionRef.current = null;

    // A new span. The caller's base is snapshotted exactly once, here, and no
    // restart below re-takes it.
    committedRef.current = "";
    sessionFinalsRef.current = "";
    silentSessionsRef.current = 0;

    onBeforeStartRef.current?.();
    listeningRef.current = true;
    setIsListening(true);
    beginSession();
  }, [beginSession]);

  const toggle = useCallback(() => {
    if (listeningRef.current) {
      stop();
    } else {
      start();
    }
  }, [start, stop]);

  useEffect(() => {
    return () => {
      // Clean teardown — detach handlers so abort doesn't trigger stale setState
      // or, worse, a restart into an unmounted component.
      discard(recognitionRef.current);
      recognitionRef.current = null;
    };
  }, []);

  return { isListening, isSupported, start, stop, forceStop, toggle };
}

/**
 * Drop a recognizer for good: silenced first, then aborted. Detaching before
 * aborting is the order that matters — an attached `onend` here would be read as
 * one more slice of the span and would start a replacement.
 */
function discard(rec: SpeechRecognition | null): void {
  if (!rec) return;
  rec.onresult = null;
  rec.onerror = null;
  rec.onend = null;
  rec.abort();
}
