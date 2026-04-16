import type { NavigateFn } from "@tanstack/react-router";
import { useEffect } from "react";
import type { useWebSocket } from "~/hooks/useWebSocket";
import { navigateToSession } from "~/lib/navigation";
import { findNearestActiveSession } from "~/lib/session/utils";
import { useAppStore } from "~/stores/app-store";
import { useChannelStore } from "~/stores/channel-store";
import { useChatStore } from "~/stores/chat-store";
import type { SessionMetadata } from "~/stores/chat-types";
import { usePulseStore } from "~/stores/pulse-store";

/** Subscribes to session lifecycle WS events: state, created, deleted, renamed, pr-updated. */
export function useSessionLifecycleSubscription(
  ws: ReturnType<typeof useWebSocket>,
  navigate: NavigateFn,
) {
  useEffect(() => {
    const unsubState = ws.subscribe("session.state", (payload) => {
      const store = useChatStore.getState();
      const sid: string = payload.sessionId;
      const prevMeta = store.sessions[sid]?.meta;
      const wasActive = store.activeSessionId === sid;

      // Clear pulse when session leaves running state.
      const newState = payload.state as SessionMetadata["state"];
      if (newState !== "running") {
        usePulseStore.getState().clearPulse(sid);
      }

      store.setSessionState(sid, newState, {
        connected: payload.connected,
        hasDirtyWorktree: payload.hasDirtyWorktree,
        worktreeMerged: payload.worktreeMerged,
        completedAt: payload.completedAt,
        hasUncommitted: payload.hasUncommitted,
        commitsAhead: payload.commitsAhead,
        commitsBehind: payload.commitsBehind,
        branchMissing: payload.branchMissing,
        mergeStatus: payload.mergeStatus as SessionMetadata["mergeStatus"],
        mergeConflictFiles: payload.mergeConflictFiles,
        gitOperation: payload.gitOperation ?? "",
        gitVersion: payload.version,
      });
      useChannelStore.getState().updateMemberState(sid, payload.state, payload.connected);

      const becameCompleted = payload.completedAt && prevMeta && !prevMeta.completedAt;
      if (becameCompleted && wasActive) {
        const projectId = prevMeta.projectId;
        const projectSlug =
          useAppStore.getState().projects.find((p) => p.id === projectId)?.slug ?? projectId;
        const sibling = findNearestActiveSession(useChatStore.getState().sessions, sid, projectId);
        if (sibling) {
          navigateToSession(navigate, projectSlug, sibling);
        } else {
          navigate({
            to: "/project/$projectSlug/session/new",
            params: { projectSlug },
          });
        }
      }
    });

    const unsubCreated = ws.subscribe("session.created", (payload) => {
      const store = useChatStore.getState();
      if (store.sessions[payload.id]) return;
      store.addSession({
        ...payload,
        state: payload.state as SessionMetadata["state"],
      } as SessionMetadata);
      useChatStore.getState().flushPendingState(payload.id);
    });

    const unsubRenamed = ws.subscribe("session.renamed", (payload) => {
      useChatStore.getState().setSessionName(payload.sessionId, payload.name);
    });

    const unsubDeleted = ws.subscribe("session.deleted", (payload) => {
      const store = useChatStore.getState();
      const deletedId: string = payload.sessionId;
      const deletedSession = store.sessions[deletedId];
      const wasActive = store.activeSessionId === deletedId;

      const deletedChannelIds = deletedSession?.meta.channelIds;
      if (deletedChannelIds) {
        for (const chId of deletedChannelIds) {
          useChannelStore.getState().removeMember(chId, deletedId);
        }
      }

      store.removeSession(deletedId);

      if (wasActive && deletedSession) {
        const projectId = deletedSession.meta.projectId;
        const projectSlug =
          useAppStore.getState().projects.find((p) => p.id === projectId)?.slug ?? projectId;
        const sibling = findNearestActiveSession(store.sessions, deletedId, projectId);
        if (sibling) {
          navigateToSession(navigate, projectSlug, sibling);
        } else {
          navigate({ to: "/project/$projectSlug", params: { projectSlug } });
        }
      }
    });

    const unsubPrUpdated = ws.subscribe("session.pr-updated", (payload) => {
      useChatStore.getState().setSessionPrUrl(payload.sessionId, payload.prUrl);
    });

    const unsubPulse = ws.subscribe("session.pulse", (payload) => {
      usePulseStore.getState().setPulse(payload.sessionId, payload);
    });

    return () => {
      unsubState();
      unsubCreated();
      unsubRenamed();
      unsubDeleted();
      unsubPrUpdated();
      unsubPulse();
    };
  }, [ws, navigate]);
}
