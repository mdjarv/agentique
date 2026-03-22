import { useCallback, useEffect } from "react";
import { useWebSocket } from "~/hooks/useWebSocket";
import { createSession, stopSession } from "~/lib/session-actions";
import { type ChatEvent, type SessionMetadata, type Turn, useChatStore } from "~/stores/chat-store";

interface SessionListResult {
  sessions: SessionMetadata[];
}

interface HistoryEvent {
  type: string;
  content?: string;
  id?: string;
  toolUseId?: string;
  name?: string;
  input?: unknown;
  costUsd?: number;
  duration?: number;
  usage?: { inputTokens: number; outputTokens: number };
  stopReason?: string;
  fatal?: boolean;
}

interface HistoryTurn {
  prompt: string;
  events: HistoryEvent[];
}

interface SessionHistoryResult {
  turns: HistoryTurn[];
}

function historyToTurns(history: HistoryTurn[]): Turn[] {
  return history.map((ht) => ({
    id: crypto.randomUUID(),
    prompt: ht.prompt,
    events: ht.events.map(
      (e): ChatEvent => ({
        id: crypto.randomUUID(),
        type: e.type as ChatEvent["type"],
        content: e.content,
        toolId: e.id || e.toolUseId,
        toolName: e.name,
        toolInput: e.input,
        cost: e.costUsd,
        duration: e.duration,
        usage: e.usage,
        stopReason: e.stopReason,
        fatal: e.fatal,
      }),
    ),
    complete: ht.events.some((e) => e.type === "result"),
  }));
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
              // Subscribe first (may trigger resume ~30-40s), then fetch history.
              ws.request("session.subscribe", { sessionId: sess.id }, 120000)
                .then(() =>
                  ws.request<SessionHistoryResult>("session.history", { sessionId: sess.id }),
                )
                .then((hist) => {
                  if (hist.turns.length > 0) {
                    useChatStore.getState().setSessionHistory(sess.id, historyToTurns(hist.turns));
                  }
                })
                .catch(() => {});
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

    // biome-ignore lint/suspicious/noExplicitAny: untyped server push payload
    const unsubRenamed = ws.subscribe("session.renamed", (payload: any) => {
      useChatStore.getState().setSessionName(payload.sessionId, payload.name);
    });

    return () => {
      unsubEvent();
      unsubState();
      unsubRenamed();
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

      // Skip stopped/done sessions
      if (activeId) {
        const activeState = useChatStore.getState().sessions[activeId]?.meta.state;
        if (activeState === "stopped" || activeState === "done") {
          activeId = null;
        }
      }

      // If no active session, create a draft then promote it
      if (!activeId) {
        useChatStore.getState().createDraft(projectId);
        activeId = useChatStore.getState().activeSessionId;
        if (!activeId) return;
      }

      // Promote draft to real backend session
      if (activeId?.startsWith("draft-")) {
        const draftMeta = useChatStore.getState().sessions[activeId]?.meta;
        const worktree = draftMeta?.worktree ?? false;
        try {
          const realId = await createSession(ws, projectId, "", worktree);
          useChatStore.getState().removeSession(activeId);
          useChatStore.getState().setActiveSessionId(realId);
          activeId = realId;
        } catch (err) {
          console.error("Failed to create session from draft:", err);
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
    [ws, projectId],
  );

  const stopSessionCb = useCallback((sessionId: string) => stopSession(ws, sessionId), [ws]);

  return { sendQuery, createSession: createSessionCb, stopSession: stopSessionCb };
}
