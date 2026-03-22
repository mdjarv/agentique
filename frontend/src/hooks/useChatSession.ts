import { useCallback, useEffect } from "react";
import { useWebSocket } from "~/hooks/useWebSocket";
import { type SessionMetadata, useChatStore } from "~/stores/chat-store";

interface SessionCreateResult {
  sessionId: string;
  name: string;
  state: string;
  worktreePath?: string;
  worktreeBranch?: string;
  createdAt: string;
}

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

  const createSession = useCallback(
    async (name: string, worktree: boolean, branch?: string) => {
      const result = await ws.request<SessionCreateResult>(
        "session.create",
        { projectId, name, worktree, branch },
        120000,
      );
      const meta: SessionMetadata = {
        id: result.sessionId,
        name: result.name,
        state: result.state as SessionMetadata["state"],
        worktreePath: result.worktreePath,
        worktreeBranch: result.worktreeBranch,
        createdAt: result.createdAt,
      };
      useChatStore.getState().addSession(meta);
      useChatStore.getState().setActiveSessionId(result.sessionId);
      return result.sessionId;
    },
    [projectId, ws],
  );

  const sendQuery = useCallback(
    async (prompt: string) => {
      let activeId = useChatStore.getState().activeSessionId;

      if (!activeId) {
        const sessions = Object.keys(useChatStore.getState().sessions);
        const name = `Session ${sessions.length + 1}`;
        activeId = await createSession(name, false);
      }

      useChatStore.getState().startTurn(activeId, prompt);
      try {
        await ws.request("session.query", { sessionId: activeId, prompt });
      } catch (err) {
        console.error("Failed to send query:", err);
      }
    },
    [ws, createSession],
  );

  const stopSession = useCallback(
    async (sessionId: string) => {
      try {
        await ws.request("session.stop", { sessionId });
      } catch (err) {
        console.error("Failed to stop session:", err);
      }
      useChatStore.getState().removeSession(sessionId);
    },
    [ws],
  );

  return { sendQuery, createSession, stopSession };
}
