import { useCallback, useEffect } from "react";
import { useWebSocket } from "~/hooks/useWebSocket";
import { createSession, stopSession } from "~/lib/session-actions";
import { type SessionMetadata, useChatStore } from "~/stores/chat-store";

interface SessionListResult {
  sessions: SessionMetadata[];
}

export function useChatSession(projectId: string) {
  const ws = useWebSocket();

  // On project change: reset, fetch sessions, subscribe to live ones
  // biome-ignore lint/correctness/useExhaustiveDependencies: intentionally re-subscribe only when projectId changes
  useEffect(() => {
    const s = useChatStore.getState();
    s.resetProject();

    ws.request<SessionListResult>("session.list", { projectId })
      .then((result) => {
        const s = useChatStore.getState();
        s.setSessions(result.sessions);
        const firstSession = result.sessions[0];
        if (firstSession) {
          s.setActiveSessionId(firstSession.id);
          for (const sess of result.sessions) {
            if (sess.state !== "stopped" && sess.state !== "done") {
              ws.request("session.subscribe", { sessionId: sess.id }).catch(() => {});
            }
          }
        }
      })
      .catch(console.error);

    // Route push events by sessionId
    // biome-ignore lint/suspicious/noExplicitAny: untyped server push payload
    const unsubEvent = ws.subscribe("session.event", (payload: any) => {
      const event = payload.event;
      useChatStore.getState().appendEvent(payload.sessionId, {
        id: crypto.randomUUID(),
        type: event.type,
        content: event.content,
        toolId: event.id || event.toolUseId,
        toolName: event.name,
        toolInput: event.input,
        cost: event.costUsd,
        duration: event.duration,
        usage: event.usage,
        stopReason: event.stopReason,
        fatal: event.fatal,
      });

      if (event.type === "result") {
        useChatStore.getState().completeTurn(payload.sessionId);
      }
    });

    // biome-ignore lint/suspicious/noExplicitAny: untyped server push payload
    const unsubState = ws.subscribe("session.state", (payload: any) => {
      useChatStore.getState().setSessionState(payload.sessionId, payload.state);
    });

    return () => {
      unsubEvent();
      unsubState();
    };
  }, [projectId]);

  const createSessionCb = useCallback(
    (name: string, worktree: boolean, branch?: string) =>
      createSession(ws, projectId, name, worktree, branch),
    [projectId, ws],
  );

  const sendQuery = useCallback(
    async (prompt: string) => {
      let activeId = useChatStore.getState().activeSessionId;

      // Check if the active session is still usable (not stopped/done).
      if (activeId) {
        const activeState = useChatStore.getState().sessions[activeId]?.meta.state;
        if (activeState === "stopped" || activeState === "done") {
          activeId = null;
        }
      }

      if (!activeId) {
        const sessions = Object.keys(useChatStore.getState().sessions);
        const name = `Session ${sessions.length + 1}`;
        try {
          activeId = await createSessionCb(name, false);
        } catch (err) {
          console.error("Failed to create session:", err);
          return;
        }
      }

      useChatStore.getState().startTurn(activeId, prompt);
      try {
        await ws.request("session.query", { sessionId: activeId, prompt });
      } catch (err) {
        console.error("Failed to send query:", err);
        useChatStore.getState().setSessionState(activeId, "idle");
      }
    },
    [ws, createSessionCb],
  );

  const stopSessionCb = useCallback((sessionId: string) => stopSession(ws, sessionId), [ws]);

  return { sendQuery, createSession: createSessionCb, stopSession: stopSessionCb };
}
