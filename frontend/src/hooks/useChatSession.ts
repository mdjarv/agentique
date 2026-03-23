import { useCallback, useEffect } from "react";
import { toast } from "sonner";
import { useWebSocket } from "~/hooks/useWebSocket";
import { parseServerEvent } from "~/lib/events";
import { createSession, interruptSession, stopSession } from "~/lib/session-actions";
import { copyToClipboard, uuid } from "~/lib/utils";
import {
  type Attachment,
  type SessionMetadata,
  type Turn,
  useChatStore,
} from "~/stores/chat-store";
import { useStreamingStore } from "~/stores/streaming-store";

interface SessionListResult {
  sessions: SessionMetadata[];
}

interface HistoryTurn {
  prompt: string;
  attachments?: Attachment[];
  events: Record<string, unknown>[];
}

interface SessionHistoryResult {
  turns: HistoryTurn[];
}

function historyToTurns(history: HistoryTurn[]): Turn[] {
  return history.map((ht) => ({
    id: uuid(),
    prompt: ht.prompt,
    attachments: ht.attachments ?? [],
    events: ht.events.map(parseServerEvent),
    complete: ht.events.some((e) => e.type === "result"),
  }));
}

export function useChatSession(projectId: string, initialSessionId?: string) {
  const ws = useWebSocket();

  // biome-ignore lint/correctness/useExhaustiveDependencies: re-subscribe only on projectId change
  useEffect(() => {
    let stale = false;
    const s = useChatStore.getState();
    s.resetProject();

    // Register for all project events via broadcast hub.
    ws.request("project.subscribe", { projectId }).catch(console.error);

    // Fetch session list and load active session history.
    ws.request<SessionListResult>("session.list", { projectId })
      .then((result) => {
        if (stale) return;
        const s = useChatStore.getState();
        s.setSessions(result.sessions);
        const target =
          (initialSessionId && result.sessions.find((sess) => sess.id === initialSessionId)) ||
          result.sessions[0];
        if (target) {
          s.setActiveSessionId(target.id);
          loadSessionHistory(target.id);
        }
      })
      .catch(console.error);

    // Push handlers for live events (broadcast to all project clients).
    // biome-ignore lint/suspicious/noExplicitAny: untyped server push payload
    const unsubEvent = ws.subscribe("session.event", (payload: any) => {
      const event = parseServerEvent(payload.event);
      useChatStore.getState().handleServerEvent(payload.sessionId, event);
      if (event.type === "text" && event.content) {
        useStreamingStore.getState().appendText(payload.sessionId, event.content);
      }
      if (event.type === "result") {
        useStreamingStore.getState().clearText(payload.sessionId);
      }
    });

    // biome-ignore lint/suspicious/noExplicitAny: untyped server push payload
    const unsubState = ws.subscribe("session.state", (payload: any) => {
      useChatStore
        .getState()
        .setSessionState(payload.sessionId, payload.state, payload.hasDirtyWorktree);
    });

    // biome-ignore lint/suspicious/noExplicitAny: untyped server push payload
    const unsubRenamed = ws.subscribe("session.renamed", (payload: any) => {
      useChatStore.getState().setSessionName(payload.sessionId, payload.name);
    });

    // biome-ignore lint/suspicious/noExplicitAny: untyped server push payload
    const unsubDeleted = ws.subscribe("session.deleted", (payload: any) => {
      useChatStore.getState().removeSession(payload.sessionId);
    });

    const unsubPermission = ws.subscribe(
      "session.tool-permission",
      // biome-ignore lint/suspicious/noExplicitAny: untyped server push payload
      (payload: any) => {
        useChatStore.getState().setPendingApproval(payload.sessionId, {
          approvalId: payload.approvalId,
          toolName: payload.toolName,
          input: payload.input,
        });
      },
    );

    const unsubQuestion = ws.subscribe(
      "session.user-question",
      // biome-ignore lint/suspicious/noExplicitAny: untyped server push payload
      (payload: any) => {
        useChatStore.getState().setPendingQuestion(payload.sessionId, {
          questionId: payload.questionId,
          questions: payload.questions,
        });
      },
    );

    // Re-subscribe and refresh state on reconnect.
    const unsubReconnect = ws.onConnect(() => {
      if (stale) return;
      ws.request("project.subscribe", { projectId }).catch(console.error);

      ws.request<SessionListResult>("session.list", { projectId })
        .then((result) => {
          if (stale) return;
          const s = useChatStore.getState();
          s.setSessions(result.sessions);
          const activeId = s.activeSessionId;
          if (activeId && result.sessions.some((sess) => sess.id === activeId)) {
            loadSessionHistory(activeId);
          }
        })
        .catch(console.error);
    });

    return () => {
      stale = true;
      unsubEvent();
      unsubState();
      unsubRenamed();
      unsubDeleted();
      unsubPermission();
      unsubQuestion();
      unsubReconnect();
    };
  }, [projectId]);

  const loadSessionHistory = useCallback(
    (sessionId: string) => {
      const store = useChatStore.getState();
      const session = store.sessions[sessionId];
      if (!session || session.turns.length > 0) return;
      if (store.historyLoading.has(sessionId)) return;

      store.setHistoryLoading(sessionId, true);
      ws.request<SessionHistoryResult>("session.history", { sessionId })
        .then((hist) => {
          if (hist.turns.length > 0) {
            useChatStore.getState().setSessionHistory(sessionId, historyToTurns(hist.turns));
          } else {
            useChatStore.getState().setHistoryLoading(sessionId, false);
          }
        })
        .catch((err) => {
          useChatStore.getState().setHistoryLoading(sessionId, false);
          console.error("Failed to load session history:", err);
        });
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
          const draftId = sessionId;
          const realId = await createSession(ws, projectId, "", worktree, {
            planMode: session?.planMode,
            autoApprove: session?.autoApprove,
          });
          if (draftId) {
            useChatStore.getState().removeSession(draftId);
          }
          sessionId = realId;
        }

        useStreamingStore.getState().clearText(sessionId);
        useChatStore.getState().submitQuery(sessionId, prompt, attachments);

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
    async (name: string, worktree: boolean, branch?: string) => {
      return createSession(ws, projectId, name, worktree, { branch });
    },
    [projectId, ws],
  );

  const interruptSessionCb = useCallback(
    (sessionId: string) => interruptSession(ws, sessionId),
    [ws],
  );

  const stopSessionCb = useCallback((sessionId: string) => stopSession(ws, sessionId), [ws]);

  return {
    sendQuery,
    createSession: createSessionCb,
    interruptSession: interruptSessionCb,
    stopSession: stopSessionCb,
    loadHistory: loadSessionHistory,
  };
}
