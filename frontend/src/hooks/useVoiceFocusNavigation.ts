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

export function useVoiceFocusNavigation(): void {
  const navigate = useNavigate();
  const focusSeq = useVoiceStore((s) => s.focusSeq);
  const seenRef = useRef(focusSeq);

  useEffect(() => {
    if (focusSeq === seenRef.current) return; // skip the initial value
    seenRef.current = focusSeq;

    const sessionId = useVoiceStore.getState().focusSessionId;
    if (!sessionId) return;
    const meta = useChatStore.getState().sessions[sessionId]?.meta;
    if (!meta) return;
    // The slug is the physical project's — machine-qualified for a remote
    // checkout, which is exactly what the route param wants.
    const project = useAppStore.getState().projects.find((p) => p.id === meta.projectId);
    if (!project) return;
    navigateToSession(navigate, project.slug, sessionId);
  }, [focusSeq, navigate]);
}
