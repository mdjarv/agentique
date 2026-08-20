import { useMemo } from "react";
import { type AgentRun, collectAgentRuns } from "~/lib/agent-runs";
import { useChatStore } from "~/stores/chat-store";

/**
 * The session's subagent roster, folded from its turns + live stream.
 *
 * The fold runs in `useMemo` over two referentially-stable store slices —
 * never inside the selector, which would hand Zustand a fresh array every
 * render and loop.
 */
export function useAgentRuns(sessionId: string): AgentRun[] {
  const turns = useChatStore((s) => s.sessions[sessionId]?.turns);
  const streamingEvents = useChatStore((s) => s.sessions[sessionId]?.streamingEvents);
  return useMemo(() => collectAgentRuns(turns, streamingEvents), [turns, streamingEvents]);
}
