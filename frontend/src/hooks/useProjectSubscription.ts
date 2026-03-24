import { useEffect } from "react";
import { toast } from "sonner";
import { useWebSocket } from "~/hooks/useWebSocket";
import { parseServerEvent } from "~/lib/events";
import { submitQuery } from "~/lib/session-actions";
import { loadSessionHistory } from "~/lib/session-history";
import { copyToClipboard } from "~/lib/utils";
import type { SessionMetadata } from "~/stores/chat-store";
import { useChatStore } from "~/stores/chat-store";
import { useStreamingStore } from "~/stores/streaming-store";

interface SessionListResult {
  sessions: SessionMetadata[];
}

/** Route raw Claude API stream deltas to the streaming store. */
function handleStreamDelta(sessionId: string, rawEvent: Record<string, unknown>) {
  try {
    // biome-ignore lint/suspicious/noExplicitAny: raw Claude API shape
    const inner = rawEvent.event as any;
    if (!inner || typeof inner !== "object") return;

    const type: string = inner.type;
    if (type === "content_block_start") {
      if (inner.content_block?.type === "tool_use") {
        useStreamingStore.getState().startToolBlock(sessionId, inner.index, inner.content_block.id);
      } else if (inner.content_block?.type === "text") {
        const existing = useStreamingStore.getState().texts[sessionId];
        if (existing) {
          useStreamingStore.getState().appendText(sessionId, "\n\n");
        }
      }
      return;
    }

    if (type === "content_block_delta") {
      const delta = inner.delta;
      if (!delta) return;
      if (delta.type === "input_json_delta" && typeof delta.partial_json === "string") {
        useStreamingStore.getState().appendToolInput(sessionId, inner.index, delta.partial_json);
      } else if (delta.type === "text_delta" && typeof delta.text === "string") {
        useStreamingStore.getState().appendText(sessionId, delta.text);
      }
    }
  } catch {
    // Ignore malformed stream events
  }
}

export function useProjectSubscription(projectId: string) {
  const ws = useWebSocket();

  // biome-ignore lint/correctness/useExhaustiveDependencies: re-subscribe only on projectId change
  useEffect(() => {
    let stale = false;
    useChatStore.getState().resetProject();

    ws.request("project.subscribe", { projectId }).catch(console.error);

    ws.request<SessionListResult>("session.list", { projectId })
      .then((result) => {
        if (stale) return;
        useChatStore.getState().setSessions(result.sessions, projectId);
      })
      .catch(console.error);

    // biome-ignore lint/suspicious/noExplicitAny: untyped server push payload
    const unsubEvent = ws.subscribe("session.event", (payload: any) => {
      const event = parseServerEvent(payload.event);
      const sid: string = payload.sessionId;
      const streaming = useStreamingStore.getState();

      if (event.type === "stream") {
        handleStreamDelta(sid, payload.event);
        return;
      }

      useChatStore.getState().handleServerEvent(sid, event);

      if (event.type === "tool_use" && event.toolId) {
        streaming.clearToolInput(sid, event.toolId);
      }
      if (event.type === "result") {
        streaming.clearText(sid);
        streaming.clearAllToolInputs(sid);

        // Auto-dispatch next queued message now that the turn is complete
        const chatStore = useChatStore.getState();
        const sess = chatStore.sessions[sid];
        const next = sess?.queuedMessages[0];
        if (next) {
          chatStore.dequeueMessage(sid);
          queueMicrotask(async () => {
            try {
              await submitQuery(ws, sid, next.prompt, next.attachments);
            } catch (err) {
              const msg = err instanceof Error ? err.message : "Unknown error";
              toast.error(msg, {
                action: { label: "Copy", onClick: () => copyToClipboard(msg) },
              });
              useChatStore.getState().setSessionState(sid, "idle");
              useChatStore.getState().clearQueue(sid);
            }
          });
        }
      }
    });

    // biome-ignore lint/suspicious/noExplicitAny: untyped server push payload
    const unsubState = ws.subscribe("session.state", (payload: any) => {
      useChatStore.getState().setSessionState(payload.sessionId, payload.state, {
        hasDirtyWorktree: payload.hasDirtyWorktree,
        worktreeMerged: payload.worktreeMerged,
        hasUncommitted: payload.hasUncommitted,
        commitsAhead: payload.commitsAhead,
        branchMissing: payload.branchMissing,
      });
    });

    // biome-ignore lint/suspicious/noExplicitAny: untyped server push payload
    const unsubRenamed = ws.subscribe("session.renamed", (payload: any) => {
      useChatStore.getState().setSessionName(payload.sessionId, payload.name);
    });

    // biome-ignore lint/suspicious/noExplicitAny: untyped server push payload
    const unsubDeleted = ws.subscribe("session.deleted", (payload: any) => {
      useChatStore.getState().removeSession(payload.sessionId);
    });

    // biome-ignore lint/suspicious/noExplicitAny: untyped server push payload
    const unsubPrUpdated = ws.subscribe("session.pr-updated", (payload: any) => {
      useChatStore.getState().setSessionPrUrl(payload.sessionId, payload.prUrl);
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

    const unsubReconnect = ws.onConnect(() => {
      if (stale) return;
      ws.request("project.subscribe", { projectId }).catch(console.error);

      ws.request<SessionListResult>("session.list", { projectId })
        .then((result) => {
          if (stale) return;
          useChatStore.getState().setSessions(result.sessions, projectId);
          const activeId = useChatStore.getState().activeSessionId;
          if (activeId && result.sessions.some((sess) => sess.id === activeId)) {
            loadSessionHistory(ws, activeId);
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
      unsubPrUpdated();
      unsubPermission();
      unsubQuestion();
      unsubReconnect();
    };
  }, [projectId]);
}
