import { useCallback, useEffect } from "react";
import { toast } from "sonner";
import { useWebSocket } from "~/hooks/useWebSocket";
import { createSession, stopSession } from "~/lib/session-actions";
import { copyToClipboard, uuid } from "~/lib/utils";
import {
  type Attachment,
  type ChatEvent,
  type SessionMetadata,
  type Turn,
  useChatStore,
} from "~/stores/chat-store";

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

interface HistoryAttachment {
  id: string;
  name: string;
  mimeType: string;
  dataUrl: string;
}

interface HistoryTurn {
  prompt: string;
  attachments?: HistoryAttachment[];
  events: HistoryEvent[];
}

interface SessionHistoryResult {
  turns: HistoryTurn[];
}

function historyToTurns(history: HistoryTurn[]): Turn[] {
  return history.map((ht) => ({
    id: uuid(),
    prompt: ht.prompt,
    attachments: (ht.attachments ?? []).map((a) => ({
      id: a.id,
      name: a.name,
      mimeType: a.mimeType,
      dataUrl: a.dataUrl,
    })),
    events: ht.events.map(
      (e): ChatEvent => ({
        id: uuid(),
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

  // biome-ignore lint/correctness/useExhaustiveDependencies: re-subscribe only on projectId change
  useEffect(() => {
    const s = useChatStore.getState();
    s.resetProject();

    // Register for all project events via broadcast hub.
    ws.request("project.subscribe", { projectId }).catch(console.error);

    // Fetch session list and load active session history.
    ws.request<SessionListResult>("session.list", { projectId })
      .then((result) => {
        const s = useChatStore.getState();
        s.setSessions(result.sessions);
        const first = result.sessions[0];
        if (first) {
          s.setActiveSessionId(first.id);
          loadSessionHistory(first.id);
        }
      })
      .catch(console.error);

    // Push handlers for live events (broadcast to all project clients).
    // biome-ignore lint/suspicious/noExplicitAny: untyped server push payload
    const unsubEvent = ws.subscribe("session.event", (payload: any) => {
      const event = payload.event;
      useChatStore.getState().appendEvent(payload.sessionId, {
        id: uuid(),
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

    // Re-subscribe on reconnect.
    const unsubReconnect = ws.onConnect(() => {
      ws.request("project.subscribe", { projectId }).catch(console.error);
    });

    return () => {
      unsubEvent();
      unsubState();
      unsubRenamed();
      unsubReconnect();
    };
  }, [projectId]);

  const loadSessionHistory = useCallback(
    (sessionId: string) => {
      const session = useChatStore.getState().sessions[sessionId];
      if (!session || session.turns.length > 0) return;

      ws.request<SessionHistoryResult>("session.history", { sessionId })
        .then((hist) => {
          if (hist.turns.length > 0) {
            useChatStore.getState().setSessionHistory(sessionId, historyToTurns(hist.turns));
          }
        })
        .catch(() => {});
    },
    [ws],
  );

  const sendQuery = useCallback(
    async (prompt: string, attachments?: Attachment[]) => {
      const store = useChatStore.getState();
      let sessionId = store.activeSessionId;
      const session = sessionId ? store.sessions[sessionId] : undefined;
      const state = session?.meta.state;

      try {
        if (!sessionId || state === "draft") {
          const worktree = session?.meta.worktree ?? false;
          const realId = await createSession(ws, projectId, "", worktree);
          if (sessionId) useChatStore.getState().removeSession(sessionId);
          useChatStore.getState().setActiveSessionId(realId);
          sessionId = realId;
        }

        // Backend handles lazy resume if session is not live.
        useChatStore.getState().startTurn(sessionId, prompt, attachments);

        const payload: Record<string, unknown> = { sessionId, prompt };
        if (attachments && attachments.length > 0) {
          payload.attachments = attachments.map((a) => ({
            name: a.name,
            mimeType: a.mimeType,
            dataUrl: a.dataUrl,
          }));
        }
        await ws.request("session.query", payload);
      } catch (err) {
        const msg = err instanceof Error ? err.message : "Unknown error";
        toast.error(msg, {
          action: { label: "Copy", onClick: () => copyToClipboard(msg) },
        });
        if (sessionId) {
          useChatStore.getState().setSessionState(sessionId, "idle");
        }
      }
    },
    [ws, projectId],
  );

  const createSessionCb = useCallback(
    (name: string, worktree: boolean, branch?: string) =>
      createSession(ws, projectId, name, worktree, branch),
    [projectId, ws],
  );

  const stopSessionCb = useCallback((sessionId: string) => stopSession(ws, sessionId), [ws]);

  return {
    sendQuery,
    createSession: createSessionCb,
    stopSession: stopSessionCb,
    loadHistory: loadSessionHistory,
  };
}
