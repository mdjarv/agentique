import { useNavigate } from "@tanstack/react-router";
import { useCallback, useEffect, useRef, useState } from "react";
import { toast } from "sonner";
import { ApprovalBanner } from "~/components/chat/ApprovalBanner";
import { type ComposerHandle, MessageComposer } from "~/components/chat/MessageComposer";
import { MessageList } from "~/components/chat/MessageList";
import { MessageQueue } from "~/components/chat/MessageQueue";
import { QuestionBanner } from "~/components/chat/QuestionBanner";
import { RateLimitBanner } from "~/components/chat/RateLimitBanner";
import { SessionHeader } from "~/components/chat/SessionHeader";
import { CollapsedTodoStrip, TodoPanel } from "~/components/chat/TodoPanel";
import { useWebSocket } from "~/hooks/useWebSocket";
import { setAutoApprove, setPermissionMode, submitQuery } from "~/lib/session-actions";
import { loadSessionHistory } from "~/lib/session-history";
import { copyToClipboard } from "~/lib/utils";
import { useAppStore } from "~/stores/app-store";
import type { Attachment } from "~/stores/chat-store";
import { useChatStore } from "~/stores/chat-store";
import { useStreamingStore } from "~/stores/streaming-store";

interface ChatPanelProps {
  projectId: string;
  sessionId: string;
}

const resumePlaceholders: Record<string, string> = {
  stopped: "Session stopped — send a message to resume...",
  done: "Session complete — send a message to continue...",
  failed: "Session failed — send a message to retry...",
};

export function ChatPanel({ projectId, sessionId }: ChatPanelProps) {
  const navigate = useNavigate();
  const ws = useWebSocket();
  const project = useAppStore((s) => s.projects.find((p) => p.id === projectId));
  const projectSlug = project?.slug ?? "";
  const session = useChatStore((s) => s.sessions[sessionId]);
  const sessionListLoaded = useChatStore((s) => s.loadedProjects.has(projectId));
  const isLoadingHistory = useChatStore((s) => s.historyLoading.has(sessionId));
  const currentAssistantText = useStreamingStore((s) => s.texts[sessionId] ?? "");

  const composerRef = useRef<ComposerHandle>(null);
  const sessionState = session?.meta.state ?? "idle";
  const planMode = session?.planMode ?? false;
  const autoApprove = session?.autoApprove ?? false;
  const queuedMessages = session?.queuedMessages ?? [];
  const todos = useChatStore((s) => s.sessions[sessionId]?.todos ?? null);
  const hasTodos = todos !== null && todos.length > 0;
  const [todoPanelCollapsed, setTodoPanelCollapsed] = useState(false);

  // Auto-expand panel when new todos arrive
  const prevTodosRef = useRef(todos);
  useEffect(() => {
    if (todos && todos !== prevTodosRef.current) {
      setTodoPanelCollapsed(false);
    }
    prevTodosRef.current = todos;
  }, [todos]);

  // Set this session as active for unseen-completion tracking
  useEffect(() => {
    useChatStore.getState().setActiveSessionId(sessionId);
    return () => {
      const s = useChatStore.getState();
      if (s.activeSessionId === sessionId) {
        s.setActiveSessionId(null);
      }
    };
  }, [sessionId]);

  // Load history on mount or session switch
  // Gates on !!session so the effect re-fires once the session list arrives
  // (on direct navigation, session is undefined when this first runs)
  const sessionExists = !!session;
  const hasTurns = (session?.turns.length ?? 0) > 0;
  useEffect(() => {
    if (sessionExists && !hasTurns) {
      loadSessionHistory(ws, sessionId);
    }
  }, [ws, sessionId, sessionExists, hasTurns]);

  // Redirect if session was deleted or doesn't exist
  useEffect(() => {
    if (sessionListLoaded && !session) {
      navigate({
        to: "/project/$projectSlug",
        params: { projectSlug },
        replace: true,
      });
    }
  }, [sessionListLoaded, session, navigate, projectSlug]);

  const handlePlanModeChange = useCallback(
    (enabled: boolean) => {
      useChatStore.getState().setSessionPlanMode(sessionId, enabled);
      const mode = enabled ? "plan" : "default";
      setPermissionMode(ws, sessionId, mode).catch((err) => {
        toast.error(err instanceof Error ? err.message : "Failed to set plan mode");
      });
    },
    [ws, sessionId],
  );

  const handleAutoApproveChange = useCallback(
    (enabled: boolean) => {
      useChatStore.getState().setSessionAutoApprove(sessionId, enabled);
      setAutoApprove(ws, sessionId, enabled).catch((err) => {
        toast.error(err instanceof Error ? err.message : "Failed to set auto-approve");
      });
    },
    [ws, sessionId],
  );

  const handleSend = useCallback(
    async (prompt: string, attachments?: Attachment[]) => {
      if (sessionState === "running") {
        useChatStore.getState().enqueueMessage(sessionId, prompt, attachments);
        return;
      }
      try {
        await submitQuery(ws, sessionId, prompt, attachments);
      } catch (err) {
        const msg = err instanceof Error ? err.message : "Unknown error";
        toast.error(msg, {
          action: { label: "Copy", onClick: () => copyToClipboard(msg) },
        });
        useChatStore.getState().setSessionState(sessionId, "idle");
      }
    },
    [ws, sessionId, sessionState],
  );

  const handleInterrupt = useCallback(async () => {
    if (queuedMessages.length > 0) {
      const text = queuedMessages.map((m) => m.prompt).join("\n\n");
      useChatStore.getState().clearQueue(sessionId);
      composerRef.current?.setText(text);
    }
    const { interruptSession } = await import("~/lib/session-actions");
    interruptSession(ws, sessionId).catch(console.error);
  }, [ws, sessionId, queuedMessages]);

  // Flush queued messages back to composer when session reaches a terminal state
  const prevStateRef = useRef(sessionState);
  useEffect(() => {
    const prev = prevStateRef.current;
    prevStateRef.current = sessionState;
    if (
      prev === "running" &&
      (sessionState === "done" || sessionState === "failed" || sessionState === "stopped")
    ) {
      if (queuedMessages.length > 0) {
        const text = queuedMessages.map((m) => m.prompt).join("\n\n");
        useChatStore.getState().clearQueue(sessionId);
        composerRef.current?.setText(text);
      }
    }
  }, [sessionState, sessionId, queuedMessages]);

  if (!session) {
    return (
      <div className="flex flex-col h-full items-center justify-center text-muted-foreground">
        <p className="text-sm">Loading session...</p>
      </div>
    );
  }

  return (
    <div className="flex h-full" data-project-id={projectId}>
      <div className="flex-1 flex flex-col min-w-0 h-full">
        <SessionHeader session={session} onSendMessage={handleSend} />
        <MessageList
          turns={session.turns}
          sessionId={sessionId}
          projectId={projectId}
          currentAssistantText={currentAssistantText}
          sessionState={sessionState}
          projectPath={project?.path}
          worktreePath={session.meta.worktreePath}
          isLoadingHistory={isLoadingHistory}
        />
        {session.pendingApproval && (
          <ApprovalBanner
            sessionId={sessionId}
            approval={session.pendingApproval}
            projectPath={project?.path}
            worktreePath={session.meta.worktreePath}
          />
        )}
        {session.pendingQuestion && (
          <QuestionBanner sessionId={sessionId} pending={session.pendingQuestion} />
        )}
        {session.rateLimit && <RateLimitBanner rateLimit={session.rateLimit} />}
        {queuedMessages.length > 0 && (
          <MessageQueue
            messages={queuedMessages}
            onCancel={(msg) => {
              useChatStore.getState().cancelQueuedMessage(sessionId, msg.id);
              composerRef.current?.setText(msg.prompt);
            }}
          />
        )}
        <MessageComposer
          ref={composerRef}
          onSend={handleSend}
          isRunning={sessionState === "running"}
          onInterrupt={handleInterrupt}
          placeholder={resumePlaceholders[sessionState]}
          planMode={planMode}
          onPlanModeChange={handlePlanModeChange}
          autoApprove={autoApprove}
          onAutoApproveChange={handleAutoApproveChange}
        />
      </div>
      {hasTodos &&
        (todoPanelCollapsed ? (
          <CollapsedTodoStrip todos={todos} onExpand={() => setTodoPanelCollapsed(false)} />
        ) : (
          <TodoPanel todos={todos} onCollapse={() => setTodoPanelCollapsed(true)} />
        ))}
    </div>
  );
}
