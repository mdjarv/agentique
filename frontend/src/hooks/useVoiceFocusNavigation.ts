/**
 * The screen follows the voice.
 *
 * A `focus` frame is an instruction to go somewhere, so it is watched as a
 * counter rather than a value: the call may ask for a session the operator is
 * already on, having wandered away and back, and "same id as last time" must
 * still navigate.
 *
 * Routing lives here rather than in the store so the store keeps no opinion
 * about the router — it holds a socket and a log, and nothing that needs a
 * route tree to construct.
 */
import { useNavigate } from "@tanstack/react-router";
import { useEffect, useRef } from "react";
import { navigateToSession } from "~/lib/navigation";
import { useAppStore } from "~/stores/app-store";
import { useChatStore } from "~/stores/chat-store";
import { useVoiceStore } from "~/stores/voice-store";

/**
 * How long to keep looking for a session the call just focused.
 *
 * A focus frame and the session it names arrive on two different sockets: the
 * frame rides the voice socket, and the `session.created` push that puts the
 * session in the stores rides `/ws`. For a session the call just CREATED those
 * two race, and losing the race used to mean the screen simply never moved —
 * the one case where the operator has been told out loud that something is on
 * their screen.
 *
 * So resolution is retried briefly, and then gives up silently exactly as it
 * did before: a frame for a session this client genuinely cannot see is not
 * something to put an error on screen for.
 */
const RESOLVE_RETRY_MS = 250;
const RESOLVE_TIMEOUT_MS = 3_000;

export function useVoiceFocusNavigation(): void {
  const navigate = useNavigate();
  const focusSeq = useVoiceStore((s) => s.focusSeq);
  const seenRef = useRef(focusSeq);

  useEffect(() => {
    if (focusSeq === seenRef.current) return; // skip the initial value
    seenRef.current = focusSeq;

    const sessionId = useVoiceStore.getState().focusSessionId;
    if (!sessionId) return;

    // Resolving is a read of two stores, so retrying it costs nothing and the
    // first attempt is synchronous — a session that is already known navigates
    // on this tick, not on the next timer.
    const resolveSlug = (): string | undefined => {
      const meta = useChatStore.getState().sessions[sessionId]?.meta;
      if (!meta) return undefined;
      // The slug is the physical project's — machine-qualified for a remote
      // checkout, which is exactly what the route param wants.
      return useAppStore.getState().projects.find((p) => p.id === meta.projectId)?.slug;
    };

    const slug = resolveSlug();
    if (slug) {
      navigateToSession(navigate, slug, sessionId);
      return;
    }

    const deadline = Date.now() + RESOLVE_TIMEOUT_MS;
    const timer = setInterval(() => {
      const retried = resolveSlug();
      if (retried) {
        clearInterval(timer);
        navigateToSession(navigate, retried, sessionId);
        return;
      }
      if (Date.now() >= deadline) clearInterval(timer);
    }, RESOLVE_RETRY_MS);

    return () => clearInterval(timer);
  }, [focusSeq, navigate]);
}
