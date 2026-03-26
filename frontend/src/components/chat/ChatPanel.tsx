import { useNavigate } from "@tanstack/react-router";
import { useCallback, useEffect, useRef, useState } from "react";
import { toast } from "sonner";
import { ApprovalBanner } from "~/components/chat/ApprovalBanner";
import { CommitDialog } from "~/components/chat/CommitDialog";
import { ContextBar } from "~/components/chat/ContextBar";
import { CreatePRDialog } from "~/components/chat/CreatePRDialog";
import { DiffView } from "~/components/chat/DiffView";
import {
  type ComposerHandle,
  type EffortLevel,
  MessageComposer,
} from "~/components/chat/MessageComposer";
import { MessageList } from "~/components/chat/MessageList";
import { MessageQueue } from "~/components/chat/MessageQueue";
import { QuestionBanner } from "~/components/chat/QuestionBanner";
import { RateLimitBanner } from "~/components/chat/RateLimitBanner";
import { SessionHeader } from "~/components/chat/SessionHeader";
import { CollapsedSessionStrip, SessionPanel } from "~/components/chat/SessionPanel";
import { Sheet, SheetContent, SheetDescription, SheetTitle } from "~/components/ui/sheet";
import { useGitActions } from "~/hooks/useGitActions";
import { useIsMobile } from "~/hooks/useIsMobile";
import { useWebSocket } from "~/hooks/useWebSocket";
import {
  type ModelId,
  refreshGitStatus,
  setAutoApprove,
  setPermissionMode,
  setSessionModel,
  submitQuery,
} from "~/lib/session-actions";
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
  const contextUsage = useChatStore((s) => s.sessions[sessionId]?.contextUsage ?? null);
  const hasTodos = todos !== null && todos.length > 0;
  const isWorktree = !!session?.meta.worktreeBranch;
  const isDirty = session?.meta.hasUncommitted || session?.meta.hasDirtyWorktree;
  const showPanel = isWorktree || hasTodos || isDirty;

  const isMobile = useIsMobile();
  const [panelCollapsed, setPanelCollapsed] = useState(false);
  const [mobileSessionOpen, setMobileSessionOpen] = useState(false);
  const [activeDialog, setActiveDialog] = useState<"none" | "pr" | "commit">("none");

  const git = useGitActions(sessionId);

  // Reset transient UI state on session switch
  const prevSessionIdRef = useRef(sessionId);
  useEffect(() => {
    if (prevSessionIdRef.current !== sessionId) {
      prevSessionIdRef.current = sessionId;
      setMobileSessionOpen(false);
      setActiveDialog("none");
    }
  }, [sessionId]);

  // Auto-expand panel when new todos arrive
  const prevTodosRef = useRef(todos);
  useEffect(() => {
    if (todos && todos !== prevTodosRef.current) {
      setPanelCollapsed(false);
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

  // Refresh git status on session navigation (so panel data is fresh)
  useEffect(() => {
    refreshGitStatus(ws, sessionId).catch(() => {});
  }, [ws, sessionId]);

  // Load history on mount or session switch
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

  const handleModelChange = useCallback(
    (model: ModelId) => {
      setSessionModel(ws, sessionId, model).catch((err) => {
        toast.error(err instanceof Error ? err.message : "Failed to set model");
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
        <SessionHeader
          session={session}
          showPanelButton={isMobile && showPanel}
          onOpenPanel={() => setMobileSessionOpen(true)}
        />

        {/* DiffView — full width in main column, triggered from panel */}
        {git.showDiff && git.diffResult && <DiffView result={git.diffResult} />}

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
        {contextUsage && <ContextBar usage={contextUsage} />}
        <MessageComposer
          ref={composerRef}
          onSend={handleSend}
          disabled={sessionState === "merging"}
          isRunning={sessionState === "running"}
          onInterrupt={handleInterrupt}
          placeholder={
            sessionState === "merging"
              ? "Git operation in progress..."
              : resumePlaceholders[sessionState]
          }
          worktree={isWorktree}
          planMode={planMode}
          onPlanModeChange={handlePlanModeChange}
          autoApprove={autoApprove}
          onAutoApproveChange={handleAutoApproveChange}
          model={(session.meta.model as ModelId) ?? undefined}
          onModelChange={handleModelChange}
          effort={(session.meta.effort as EffortLevel) ?? ""}
        />
      </div>

      {/* Right panel — git + todos */}
      {showPanel &&
        (isMobile ? (
          <Sheet open={mobileSessionOpen} onOpenChange={setMobileSessionOpen}>
            <SheetContent side="right" className="p-0" showCloseButton={false}>
              <SheetTitle className="sr-only">Session details</SheetTitle>
              <SheetDescription className="sr-only">Git status and todos</SheetDescription>
              <SessionPanel
                meta={session.meta}
                todos={todos}
                git={git}
                onCollapse={() => setMobileSessionOpen(false)}
                onSendMessage={handleSend}
                onOpenDialog={(d) => setActiveDialog(d)}
              />
            </SheetContent>
          </Sheet>
        ) : panelCollapsed ? (
          <CollapsedSessionStrip
            meta={session.meta}
            todos={todos}
            onExpand={() => setPanelCollapsed(false)}
          />
        ) : (
          <div className="w-72 border-l shrink-0">
            <SessionPanel
              meta={session.meta}
              todos={todos}
              git={git}
              onCollapse={() => setPanelCollapsed(true)}
              onSendMessage={handleSend}
              onOpenDialog={(d) => setActiveDialog(d)}
            />
          </div>
        ))}

      {/* Dialogs */}
      <CreatePRDialog
        open={activeDialog === "pr"}
        onOpenChange={(open) => setActiveDialog(open ? "pr" : "none")}
        sessionId={sessionId}
        defaultTitle={session.meta.name}
        onSubmit={async (title, body) => {
          const ok = await git.handlePRSubmit(title, body);
          if (ok) setActiveDialog("none");
        }}
        loading={git.creatingPR}
      />
      <CommitDialog
        open={activeDialog === "commit"}
        onOpenChange={(open) => setActiveDialog(open ? "commit" : "none")}
        sessionId={sessionId}
        defaultTitle={session.meta.name}
        onSubmit={async (message) => {
          const ok = await git.handleCommit(message);
          if (ok) setActiveDialog("none");
        }}
        loading={git.committing}
      />
    </div>
  );
}
