import { useCallback, useEffect, useRef, useState } from "react";
import { VoiceCall, type VoiceCallState } from "~/lib/voice/call";
import type { VoiceTranscript } from "~/lib/voice/protocol";

export interface VoiceCallStats {
  /** Frames of microphone audio uploaded. */
  framesSent: number;
  /** Frames of engine audio received. */
  framesReceived: number;
}

/**
 * A transcript with a stable identity.
 *
 * The wire type carries none, and a list index is not one: interim lines are
 * replaced as they settle, so keying on position makes React reuse the wrong
 * node.
 */
export type KeyedTranscript = VoiceTranscript & { id: number };

export interface UseVoiceCall {
  state: VoiceCallState;
  /** Why the call failed or closed, when the server or browser said. */
  detail: string | undefined;
  transcripts: KeyedTranscript[];
  start: () => void;
  stop: () => void;
}

/**
 * Owns one [VoiceCall] for the lifetime of the component.
 *
 * The call object is a ref rather than state: it is a long-lived resource
 * holding a socket and a microphone, and putting it in state would rebuild it
 * on every render.
 */
export function useVoiceCall(url?: string): UseVoiceCall {
  const [state, setState] = useState<VoiceCallState>("idle");
  const [detail, setDetail] = useState<string | undefined>(undefined);
  const [transcripts, setTranscripts] = useState<KeyedTranscript[]>([]);
  const callRef = useRef<VoiceCall | null>(null);
  const nextIdRef = useRef(0);

  const call = useCallback(() => {
    if (!callRef.current) {
      callRef.current = new VoiceCall({
        onState: (next, why) => {
          setState(next);
          setDetail(why);
        },
        onTranscript: (t) => setTranscripts((prev) => [...prev, { ...t, id: nextIdRef.current++ }]),
      });
    }
    return callRef.current;
  }, []);

  const start = useCallback(() => {
    setTranscripts([]);
    setDetail(undefined);
    void call().start(url);
  }, [call, url]);

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

  return { state, detail, transcripts, start, stop };
}
