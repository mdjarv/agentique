import { useCallback, useEffect, useRef, useState } from "react";
import { primaryVoiceUrl, VoiceCall, type VoiceCallState } from "~/lib/voice/call";

/** One line in the call log, whatever produced it. */
export interface VoiceLogEntry {
  id: number;
  /** Where it came from — decides how it should be read, and how much to trust it. */
  source: "you" | "agent" | "dispatched" | "report" | "notice";
  text: string;
  /** report/notice kind, when there is one. */
  kind?: string;
}

export interface UseVoiceCall {
  state: VoiceCallState;
  /** Why the call failed or closed, when the server or browser said. */
  detail: string | undefined;
  log: VoiceLogEntry[];
  start: () => void;
  stop: () => void;
}

/**
 * Owns one [VoiceCall] for the lifetime of the component.
 *
 * The call object is a ref rather than state: it is a long-lived resource
 * holding a socket and a microphone, and putting it in state would rebuild it
 * on every render.
 *
 * sessionId names the session the call hands work to. Without it the call still
 * converses; it just has nowhere to dispatch.
 */
export function useVoiceCall(sessionId?: string): UseVoiceCall {
  const [state, setState] = useState<VoiceCallState>("idle");
  const [detail, setDetail] = useState<string | undefined>(undefined);
  const [log, setLog] = useState<VoiceLogEntry[]>([]);
  const callRef = useRef<VoiceCall | null>(null);
  const nextIdRef = useRef(0);

  const append = useCallback((source: VoiceLogEntry["source"], text: string, kind?: string) => {
    if (!text) return;
    setLog((prev) => [...prev, { id: nextIdRef.current++, source, text, kind }]);
  }, []);

  const call = useCallback(() => {
    if (!callRef.current) {
      callRef.current = new VoiceCall({
        onState: (next, why) => {
          setState(next);
          setDetail(why);
        },
        // Only settled transcripts are logged; interim text rewrites itself and
        // would fill the log with half-sentences.
        onTranscript: (t) => {
          if (!t.final) return;
          append(t.source === "caller" ? "you" : "agent", t.text ?? "");
        },
        onDispatched: (d) => append("dispatched", d.headline ?? "", undefined),
        onReport: (r) => append("report", r.headline ?? "", r.kind),
        onNotice: (n) => append("notice", n.headline ?? "", n.kind),
      });
    }
    return callRef.current;
  }, [append]);

  const start = useCallback(() => {
    setLog([]);
    setDetail(undefined);
    void call().start(primaryVoiceUrl(sessionId));
  }, [call, sessionId]);

  const stop = useCallback(() => {
    void call().stop();
  }, [call]);

  // Releasing the microphone on unmount is not optional: the browser's
  // recording indicator stays lit otherwise, and the socket keeps a paid
  // session open on a page nobody is looking at.
  useEffect(() => {
    return () => {
      void callRef.current?.stop();
      callRef.current = null;
    };
  }, []);

  return { state, detail, log, start, stop };
}
