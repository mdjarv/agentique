import { useNavigate } from "@tanstack/react-router";
import { useEffect, useRef } from "react";
import { toast } from "sonner";
import { useWebSocket } from "~/hooks/useWebSocket";
import { parseServerEvent } from "~/lib/events";
import type { ListSessionsResult, ProjectGitStatus, Tag } from "~/lib/generated-types";
import { getProjectGitStatus, listTags } from "~/lib/project-actions";
import { loadSessionHistory } from "~/lib/session-history";
import type { Project } from "~/lib/types";
import { sessionShortId } from "~/lib/utils";
import { useAppStore } from "~/stores/app-store";
import type { SessionMetadata } from "~/stores/chat-store";
import { useChatStore } from "~/stores/chat-store";
import { useStreamingStore } from "~/stores/streaming-store";

/** Find the most recently created idle or running session in the same project. */
function findNearestActiveSession(
  sessions: Record<string, { meta: SessionMetadata }>,
  excludeId: string,
  projectId: string,
): string | null {
  let best: { id: string; createdAt: number } | null = null;
  for (const [id, data] of Object.entries(sessions)) {
    if (id === excludeId) continue;
    if (data.meta.projectId !== projectId) continue;
    if (data.meta.state !== "idle" && data.meta.state !== "running") continue;
    const t = new Date(data.meta.createdAt).getTime();
    if (!best || t > best.createdAt) {
      best = { id, createdAt: t };
    }
  }
  return best?.id ?? null;
}

const toolBlockIndex = new Map<string, Map<number, string>>();

/** Route raw Claude API stream deltas to the streaming store. */
function handleStreamDelta(sessionId: string, rawEvent: Record<string, unknown>) {
  try {
    // biome-ignore lint/suspicious/noExplicitAny: raw Claude API shape
    const inner = rawEvent.event as any;
    if (!inner || typeof inner !== "object") return;

    const type: string = inner.type;

    if (type === "message_start") {
      const usage = inner.message?.usage;
      if (usage && typeof usage.input_tokens === "number") {
        // Total context = input + cache_read + cache_create (per-API-call prompt size)
        const contextTokens =
          usage.input_tokens +
          (typeof usage.cache_read_input_tokens === "number" ? usage.cache_read_input_tokens : 0) +
          (typeof usage.cache_creation_input_tokens === "number"
            ? usage.cache_creation_input_tokens
            : 0);
        useChatStore.getState().updateStreamingContextUsage(sessionId, {
          inputTokens: contextTokens,
        });
      }
      return;
    }

    if (type === "message_delta") {
      const usage = inner.usage;
      if (usage && typeof usage.output_tokens === "number") {
        useChatStore.getState().updateStreamingContextUsage(sessionId, {
          outputTokens: usage.output_tokens,
        });
      }
      return;
    }

    if (type === "content_block_start") {
      if (inner.content_block?.type === "tool_use") {
        let sessionMap = toolBlockIndex.get(sessionId);
        if (!sessionMap) {
          sessionMap = new Map();
          toolBlockIndex.set(sessionId, sessionMap);
        }
        sessionMap.set(inner.index, inner.content_block.id);
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
        const toolId = toolBlockIndex.get(sessionId)?.get(inner.index);
        if (toolId) {
          useStreamingStore.getState().appendToolInput(sessionId, toolId, delta.partial_json);
        }
      } else if (delta.type === "text_delta" && typeof delta.text === "string") {
        useStreamingStore.getState().appendText(sessionId, delta.text);
      }
    }
  } catch {
    // Ignore malformed stream events
  }
}

function clearToolBlockIndex(sessionId: string) {
  toolBlockIndex.delete(sessionId);
}

function subscribeAndLoad(ws: ReturnType<typeof useWebSocket>, projectId: string) {
  ws.request("project.subscribe", { projectId }).catch((err) => {
    console.error("project.subscribe failed", err);
    toast.error("Failed to subscribe to project updates");
  });
  ws.request<ListSessionsResult>("session.list", { projectId })
    .then((result) => {
      useChatStore.getState().setSessions(result.sessions as SessionMetadata[], projectId);
      for (const session of result.sessions) {
        if (!session.completedAt) {
          loadSessionHistory(ws, session.id);
        }
      }
    })
    .catch((err) => {
      console.error("session.list failed", err);
      toast.error("Failed to load sessions");
    });
  getProjectGitStatus(ws, projectId)
    .then((status) => useAppStore.getState().setProjectGitStatus(status))
    .catch(() => {}); // silent — project may not be a git repo
}

/**
 * Subscribe to ALL projects and set up global WS event listeners.
 * Mount once at the app level (e.g. in ProjectList).
 */
export function useGlobalSubscriptions(projects: Project[]) {
  const ws = useWebSocket();
  const navigate = useNavigate();
  const subscribedRef = useRef(new Set<string>());
  const projectsRef = useRef(projects);
  projectsRef.current = projects;

  // Load tags once on mount
  const tagsLoadedRef = useRef(false);
  useEffect(() => {
    if (tagsLoadedRef.current) return;
    tagsLoadedRef.current = true;
    listTags(ws)
      .then((result) => {
        useAppStore.getState().setTags(result.tags);
        useAppStore.getState().setProjectTags(result.projectTags);
      })
      .catch(() => {}); // silent — tags feature may not exist on older backend
  }, [ws]);

  // Subscribe to new projects as they appear
  useEffect(() => {
    for (const project of projects) {
      if (subscribedRef.current.has(project.id)) continue;
      subscribedRef.current.add(project.id);
      subscribeAndLoad(ws, project.id);
    }
  }, [ws, projects]);

  // Global event listeners — mounted once, independent of project list
  useEffect(() => {
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
        clearToolBlockIndex(sid);
        // Queue drain is handled by the backend — no frontend round-trip needed.
      }
    });

    // biome-ignore lint/suspicious/noExplicitAny: untyped server push payload
    const unsubState = ws.subscribe("session.state", (payload: any) => {
      useChatStore.getState().setSessionState(payload.sessionId, payload.state, {
        connected: payload.connected,
        hasDirtyWorktree: payload.hasDirtyWorktree,
        worktreeMerged: payload.worktreeMerged,
        completedAt: payload.completedAt,
        hasUncommitted: payload.hasUncommitted,
        commitsAhead: payload.commitsAhead,
        commitsBehind: payload.commitsBehind,
        branchMissing: payload.branchMissing,
        mergeStatus: payload.mergeStatus,
        mergeConflictFiles: payload.mergeConflictFiles,
        gitOperation: payload.gitOperation ?? "",
        gitVersion: payload.version,
      });
    });

    // biome-ignore lint/suspicious/noExplicitAny: untyped server push payload
    const unsubCreated = ws.subscribe("session.created", (payload: any) => {
      const store = useChatStore.getState();
      if (store.sessions[payload.id]) return;
      store.addSession({
        ...payload,
        state: payload.state as SessionMetadata["state"],
      } as SessionMetadata);
    });

    // biome-ignore lint/suspicious/noExplicitAny: untyped server push payload
    const unsubRenamed = ws.subscribe("session.renamed", (payload: any) => {
      useChatStore.getState().setSessionName(payload.sessionId, payload.name);
    });

    // biome-ignore lint/suspicious/noExplicitAny: untyped server push payload
    const unsubDeleted = ws.subscribe("session.deleted", (payload: any) => {
      const store = useChatStore.getState();
      const deletedId: string = payload.sessionId;
      const deletedSession = store.sessions[deletedId];
      const wasActive = store.activeSessionId === deletedId;

      store.removeSession(deletedId);

      if (wasActive && deletedSession) {
        const projectId = deletedSession.meta.projectId;
        const projectSlug =
          useAppStore.getState().projects.find((p) => p.id === projectId)?.slug ?? projectId;
        const sibling = findNearestActiveSession(store.sessions, deletedId, projectId);
        if (sibling) {
          navigate({
            to: "/project/$projectSlug/session/$sessionShortId",
            params: { projectSlug, sessionShortId: sessionShortId(sibling) },
          });
        } else {
          navigate({ to: "/project/$projectSlug", params: { projectSlug } });
        }
      }
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

    // biome-ignore lint/suspicious/noExplicitAny: untyped server push payload
    const unsubPermMode = ws.subscribe("session.permission-mode-changed", (payload: any) => {
      useChatStore
        .getState()
        .setSessionPlanMode(payload.sessionId, payload.permissionMode === "plan");
    });

    // Backend-initiated turns: create the turn entry in the store.
    // De-duplicates with optimistic creation from submitQuery() — if the last turn
    // already has the same prompt and no events, the frontend already created it.
    // biome-ignore lint/suspicious/noExplicitAny: untyped server push payload
    const unsubTurnStarted = ws.subscribe("session.turn-started", (payload: any) => {
      const sid: string = payload.sessionId;
      useStreamingStore.getState().clearText(sid);
      const session = useChatStore.getState().sessions[sid];
      const lastTurn = session?.turns[session.turns.length - 1];
      if (
        lastTurn &&
        !lastTurn.complete &&
        lastTurn.events.length === 0 &&
        lastTurn.prompt === payload.prompt
      ) {
        return; // Already created optimistically
      }
      useChatStore.getState().submitQuery(sid, payload.prompt);
    });

    const unsubProjectGit = ws.subscribe("project.git-status", (payload: ProjectGitStatus) => {
      useAppStore.getState().setProjectGitStatus(payload);
    });

    // biome-ignore lint/suspicious/noExplicitAny: untyped server push payload
    const unsubProjectUpdated = ws.subscribe("project.updated", (payload: any) => {
      useAppStore.getState().updateProject(payload);
    });

    // biome-ignore lint/suspicious/noExplicitAny: untyped server push payload
    const unsubProjectTagsUpdated = ws.subscribe("project.tags-updated", (payload: any) => {
      const tagIds = (payload.tags as Tag[]).map((t) => t.id);
      useAppStore.getState().setTagsForProject(payload.projectId, tagIds);
    });

    const unsubTagCreated = ws.subscribe("tag.created", (payload: Tag) => {
      useAppStore.getState().addTag(payload);
    });

    const unsubTagUpdated = ws.subscribe("tag.updated", (payload: Tag) => {
      useAppStore.getState().updateTag(payload);
    });

    // biome-ignore lint/suspicious/noExplicitAny: untyped server push payload
    const unsubTagDeleted = ws.subscribe("tag.deleted", (payload: any) => {
      useAppStore.getState().removeTag(payload.id);
    });

    // Reload active session when returning from background (catches missed events on mobile)
    let hiddenAt = 0;
    const handleVisibility = () => {
      if (document.visibilityState === "hidden") {
        hiddenAt = Date.now();
        return;
      }
      // Only reload if hidden for >2s (avoids desktop tab-switch churn)
      if (Date.now() - hiddenAt < 2000) return;
      const activeId = useChatStore.getState().activeSessionId;
      if (activeId && ws.connectionState === "connected") {
        loadSessionHistory(ws, activeId);
      }
    };
    document.addEventListener("visibilitychange", handleVisibility);

    const unsubReconnect = ws.onConnect(() => {
      // Re-subscribe all known projects on reconnect
      subscribedRef.current.clear();
      for (const project of projectsRef.current) {
        subscribedRef.current.add(project.id);
        subscribeAndLoad(ws, project.id);
      }

      // Reload tags
      listTags(ws)
        .then((result) => {
          useAppStore.getState().setTags(result.tags);
          useAppStore.getState().setProjectTags(result.projectTags);
        })
        .catch(() => {});

      // Reload active session history
      const activeId = useChatStore.getState().activeSessionId;
      if (activeId) {
        loadSessionHistory(ws, activeId);
      }
    });

    return () => {
      unsubEvent();
      unsubState();
      unsubCreated();
      unsubRenamed();
      unsubDeleted();
      unsubPrUpdated();
      unsubPermission();
      unsubQuestion();
      unsubPermMode();
      unsubTurnStarted();
      unsubProjectGit();
      unsubProjectUpdated();
      unsubProjectTagsUpdated();
      unsubTagCreated();
      unsubTagUpdated();
      unsubTagDeleted();
      unsubReconnect();
      document.removeEventListener("visibilitychange", handleVisibility);
    };
  }, [ws, navigate]);
}
