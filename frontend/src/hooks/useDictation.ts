/**
 * Dictation, by whichever route can actually work in this browser.
 *
 * Two routes. The **browser** route is the Web Speech API
 * (`useSpeechRecognition`): instant, word by word, and nothing leaves the
 * browser's own vendor. The **server** route is `/api/voice/dictation`
 * (`ServerDictation`), which sends audio to Gemini and gets text back a
 * sentence at a time. The browser route is always tried first where it can
 * work; the server route is for where it cannot:
 *
 * - no recognizer at all (Firefox);
 * - a fault known before the press (Brave, which blocks the service);
 * - a fault the first attempt ends in (Safari with Dictation off, a service
 *   that never answers) — and then the *same press* continues on the server,
 *   so the operator is not asked to press again for a failure that was ours
 *   to route around.
 *
 * A fallback is remembered for the page's lifetime: a browser that refused
 * once will refuse the next press the same way, and trying it again only holds
 * the microphone for the length of the failure.
 *
 * Faults that are about the microphone, not the service (denied, missing),
 * apply to both routes and are reported rather than routed around.
 */
import { useCallback, useEffect, useRef, useState } from "react";
import type { DictationFault } from "~/lib/speech/dictation-fault";
import {
  type DictationEndReason,
  type DictationPhase,
  dictationUrl,
  ServerDictation,
} from "~/lib/speech/server-dictation";
import { joinTranscript } from "~/lib/speech-transcript";
import { MicCapture } from "~/lib/voice/capture";
import { useFeatureStore } from "~/stores/feature-store";
import { useSpeechRecognition } from "./useSpeechRecognition";

export type DictationRoute = "browser" | "server";

/** Faults a server route can get past, because they are about the browser's service. */
const ROUTABLE_FAULTS: ReadonlySet<DictationFault> = new Set([
  "browser-blocked",
  "service-disabled",
  "service-unreachable",
  "language-unsupported",
]);

/** Remembered across composers for the page's lifetime; see the module comment. */
let preferServer = false;

/** Ends the server route reports in terms the composer surfaces. */
export type DictationEnd = Exclude<
  DictationEndReason,
  "mic-denied" | "no-microphone" | "unavailable"
>;

interface UseDictationOptions {
  /** The whole dictated text so far, normalized; replace everything after the base. */
  onTranscript: (transcript: string) => void;
  /** Called once as a dictation starts, to snapshot what was already there. */
  onBeforeStart?: () => void;
  /** Dictation cannot work, and no route gets past it. */
  onFault?: (fault: DictationFault) => void;
  /** The browser route failed and the server route took over, for this fault. */
  onFallback?: (fault: DictationFault) => void;
  /** A server dictation ended on its own, for a reason worth saying. */
  onEnded?: (end: DictationEnd) => void;
}

export interface Dictation {
  isSupported: boolean;
  isListening: boolean;
  /** Where a server dictation is; `null` on the browser route or when idle. */
  phase: DictationPhase | null;
  route: DictationRoute;
  fault: DictationFault | null;
  start: () => void;
  stop: () => void;
  forceStop: () => void;
  toggle: () => void;
}

export function useDictation({
  onTranscript,
  onBeforeStart,
  onFault,
  onFallback,
  onEnded,
}: UseDictationOptions): Dictation {
  const serverAvailable = useFeatureStore((s) => s.features.dictation);

  const cb = useRef({ onTranscript, onBeforeStart, onFault, onFallback, onEnded });
  cb.current = { onTranscript, onBeforeStart, onFault, onFallback, onEnded };

  const [serverListening, setServerListening] = useState(false);
  const [phase, setPhase] = useState<DictationPhase | null>(null);
  const [serverFault, setServerFault] = useState<DictationFault | null>(null);
  const [routedToServer, setRoutedToServer] = useState(preferServer);
  const serverRef = useRef<ServerDictation | null>(null);
  const serverAvailableRef = useRef(serverAvailable);
  serverAvailableRef.current = serverAvailable;

  const startServer = useCallback(() => {
    if (serverRef.current) return;
    setServerFault(null);
    cb.current.onBeforeStart?.();

    let committed = "";
    const dictation = new ServerDictation({
      mic: () => new MicCapture(),
      socket: (url) => new WebSocket(url),
      url: dictationUrl(),
    });
    serverRef.current = dictation;
    setServerListening(true);

    void dictation.start({
      onPhase: (next) => {
        if (serverRef.current === dictation) setPhase(next);
      },
      onText: (text, newUtterance) => {
        if (serverRef.current !== dictation) return;
        // A new utterance gets exactly one space; a later chunk of the same
        // one carries the service's own spacing, and a boundary can fall
        // inside a word.
        committed = newUtterance ? joinTranscript(committed, text) : committed + text;
        committed = committed.replace(/ {2,}/g, " ");
        cb.current.onTranscript(committed.trim());
      },
      onEnd: (reason) => {
        if (serverRef.current !== dictation) return;
        serverRef.current = null;
        setServerListening(false);
        setPhase(null);
        if (!reason) return;
        const fault: DictationFault | null =
          reason === "mic-denied"
            ? "mic-denied"
            : reason === "no-microphone"
              ? "no-microphone"
              : reason === "unavailable"
                ? "server-unavailable"
                : null;
        if (fault) {
          setServerFault(fault);
          cb.current.onFault?.(fault);
          return;
        }
        cb.current.onEnded?.(reason as DictationEnd);
      },
    });
  }, []);

  const handleBrowserFault = useCallback(
    (fault: DictationFault) => {
      if (serverAvailableRef.current && ROUTABLE_FAULTS.has(fault)) {
        preferServer = true;
        setRoutedToServer(true);
        cb.current.onFallback?.(fault);
        startServer();
        return;
      }
      cb.current.onFault?.(fault);
    },
    [startServer],
  );

  const browser = useSpeechRecognition({
    onTranscript: useCallback((t: string) => cb.current.onTranscript(t), []),
    onBeforeStart: useCallback(() => cb.current.onBeforeStart?.(), []),
    onFault: handleBrowserFault,
  });

  const route: DictationRoute =
    serverAvailable && (routedToServer || !browser.isSupported) ? "server" : "browser";
  const routeRef = useRef(route);
  routeRef.current = route;

  const start = useCallback(() => {
    if (routeRef.current === "server") startServer();
    else browser.start();
  }, [browser.start, startServer]);

  const stop = useCallback(() => {
    if (serverRef.current) serverRef.current.stop();
    else browser.stop();
  }, [browser.stop]);

  const forceStop = useCallback(() => {
    const server = serverRef.current;
    serverRef.current = null;
    server?.abort();
    setServerListening(false);
    setPhase(null);
    browser.forceStop();
  }, [browser.forceStop]);

  const isListening = serverListening || browser.isListening;

  const toggle = useCallback(() => {
    if (serverRef.current || browser.isListening) stop();
    else start();
  }, [browser.isListening, start, stop]);

  useEffect(
    () => () => {
      const server = serverRef.current;
      serverRef.current = null;
      server?.abort();
    },
    [],
  );

  // A browser fault the server route gets past is not a fault here.
  const browserFault =
    browser.fault && !(serverAvailable && ROUTABLE_FAULTS.has(browser.fault))
      ? browser.fault
      : null;
  const fault = route === "server" ? serverFault : browserFault;

  return {
    isSupported: browser.isSupported || serverAvailable,
    isListening,
    phase,
    route,
    fault,
    start,
    stop,
    forceStop,
    toggle,
  };
}
