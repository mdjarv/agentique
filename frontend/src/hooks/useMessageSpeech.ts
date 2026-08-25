import { useCallback, useSyncExternalStore } from "react";
import { speaker } from "~/lib/speech/speaker";

/**
 * Whether this message is the one currently being read aloud.
 *
 * Subscribes to the module-level speaker rather than holding local state,
 * because speech is a serial channel: starting one message stops another, and
 * that other component has to see its button go back to idle.
 */
export function useMessageSpeech(id: string, getMarkdown: () => string) {
  const speakingId = useSyncExternalStore(
    useCallback((onChange) => speaker.subscribe(onChange), []),
    () => speaker.speakingId,
    () => null, // server render: nothing is speaking
  );

  const toggle = useCallback(() => {
    speaker.toggle(id, getMarkdown());
  }, [id, getMarkdown]);

  return {
    supported: speaker.supported,
    isSpeaking: speakingId === id,
    toggle,
  };
}
