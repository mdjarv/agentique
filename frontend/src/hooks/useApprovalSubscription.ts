import { useEffect } from "react";
import type { useWebSocket } from "~/hooks/useWebSocket";
import { useChatStore } from "~/stores/chat-store";

/** Subscribes to approval/question WS events: tool-permission, approval-resolved, question, permission-mode. */
export function useApprovalSubscription(ws: ReturnType<typeof useWebSocket>) {
  useEffect(() => {
    const unsubPermission = ws.subscribe("session.tool-permission", (payload) => {
      useChatStore.getState().setPendingApproval(payload.sessionId, {
        approvalId: payload.approvalId,
        toolName: payload.toolName,
        input: payload.input,
      });
    });

    const unsubQuestion = ws.subscribe("session.user-question", (payload) => {
      useChatStore.getState().setPendingQuestion(payload.sessionId, {
        questionId: payload.questionId,
        questions: payload.questions,
      });
    });

    const unsubApprovalAutoResolved = ws.subscribe("session.approval-auto-resolved", (payload) => {
      useChatStore.getState().clearPendingApproval(payload.sessionId);
    });

    const unsubApprovalResolved = ws.subscribe("session.approval-resolved", (payload) => {
      useChatStore.getState().clearPendingApproval(payload.sessionId);
    });

    const unsubQuestionResolved = ws.subscribe("session.question-resolved", (payload) => {
      useChatStore.getState().clearPendingQuestion(payload.sessionId);
    });

    const unsubPermMode = ws.subscribe("session.permission-mode-changed", (payload) => {
      useChatStore
        .getState()
        .setSessionPlanMode(payload.sessionId, payload.permissionMode === "plan");
    });

    return () => {
      unsubPermission();
      unsubQuestion();
      unsubApprovalAutoResolved();
      unsubApprovalResolved();
      unsubQuestionResolved();
      unsubPermMode();
    };
  }, [ws]);
}
