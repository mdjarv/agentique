import { useNavigate } from "@tanstack/react-router";
import { FileDiff, MessageSquare } from "lucide-react";
import { useCallback, useEffect, useRef, useState } from "react";
import { toast } from "sonner";
import { ApprovalBanner } from "~/components/chat/ApprovalBanner";
import { ChangesView } from "~/components/chat/ChangesView";
import { CommitDialog } from "~/components/chat/CommitDialog";
import { ContextBar } from "~/components/chat/ContextBar";
import { CreatePRDialog } from "~/components/chat/CreatePRDialog";
import {
  type ComposerHandle,
  type EffortLevel,
  MessageComposer,
} from "~/components/chat/MessageComposer";
import { MessageList } from "~/components/chat/MessageList";
import { MessageQueue } from "~/components/chat/MessageQueue";
import { PlanReviewBanner } from "~/components/chat/PlanReviewBanner";
import { QuestionBanner } from "~/components/chat/QuestionBanner";
import { ResumeBanner } from "~/components/chat/ResumeBanner";

import { SessionHeader } from "~/components/chat/SessionHeader";
import { CollapsedSessionStrip, SessionPanel } from "~/components/chat/SessionPanel";
import { StatusPage } from "~/components/layout/PageHeader";
import { Sheet, SheetContent, SheetDescription, SheetTitle } from "~/components/ui/sheet";
import { useGitActions } from "~/hooks/useGitActions";
import { useIsMobile } from "~/hooks/useIsMobile";
import { useWebSocket } from "~/hooks/useWebSocket";
import {
  type ModelId,
  createSession,
  interruptSession,
  isGitFresh,
  refreshGitStatus,
  resumeSession,
  setAutoApprove,
  setPermissionMode,
  setSessionModel,
  stopSession,
  submitQuery,
} from "~/lib/session-actions";
import { loadSessionHistory } from "~/lib/session-history";
import { cn, copyToClipboard, getErrorMessage, sessionShortId } from "~/lib/utils";
import { useAppStore } from "~/stores/app-store";
import type { Attachment, QueuedMessage, Turn } from "~/stores/chat-store";
import { useChatStore } from "~/stores/chat-store";
import { useUIStore } from "~/stores/ui-store";

interface ChatPanelProps {
  projectId: string;
  sessionId: string;
}

const resumePlaceholders: Record<string, string> = {
  stopped: "Send a message or press Enter to resume...",
  done: "Send a message or press Enter to continue...",
  failed: "Send a message or press Enter to retry...",
};

const resumableStates = new Set(["stopped", "failed", "done"]);

const EMPTY_TURNS: Turn[] = [];
const EMPTY_QUEUE: QueuedMessage[] = [];

export function ChatPanel({ projectId, sessionId }: ChatPanelProps) {
  const navigate = useNavigate();
  const ws = useWebSocket();
  const project = useAppStore((s) => s.projects.find((p) => p.id === projectId));
  const projectSlug = project?.slug ?? "";

  // Granular selectors — turns changes on every streaming event, meta changes
  // only on state transitions and git refreshes. Splitting prevents cascading
  // re-renders during streaming.
  const turns = useChatStore((s) => s.sessions[sessionId]?.turns ?? EMPTY_TURNS);
  const meta = useChatStore((s) => s.sessions[sessionId]?.meta);
  const pendingApproval = useChatStore((s) => s.sessions[sessionId]?.pendingApproval ?? null);
  const pendingQuestion = useChatStore((s) => s.sessions[sessionId]?.pendingQuestion ?? null);
  const planMode = useChatStore((s) => s.sessions[sessionId]?.planMode ?? false);
  const autoApprove = useChatStore((s) => s.sessions[sessionId]?.autoApprove ?? false);
  const queuedMessages = useChatStore((s) => s.sessions[sessionId]?.queuedMessages ?? EMPTY_QUEUE);
  const todos = useChatStore((s) => s.sessions[sessionId]?.todos ?? null);
  const contextUsage = useChatStore((s) => s.sessions[sessionId]?.contextUsage ?? null);
  const compacting = useChatStore((s) => s.sessions[sessionId]?.compacting ?? false);
  const sessionListLoaded = useChatStore((s) => s.loadedProjects.has(projectId));
  const isLoadingHistory = useChatStore((s) => s.historyLoading.has(sessionId));

  const composerRef = useRef<ComposerHandle>(null);
  const sessionState = meta?.state ?? "idle";
  const draft = useUIStore((s) => s.drafts[sessionId] ?? "");
  const hasTodos = todos !== null && todos.length > 0;
  const isWorktree = !!meta?.worktreeBranch;
  const isDirty = meta?.hasUncommitted || meta?.hasDirtyWorktree;
  const showPanel = isWorktree || hasTodos || isDirty;

  const isMobile = useIsMobile();
  const [panelCollapsed, setPanelCollapsed] = useState(false);
  const [mobileSessionOpen, setMobileSessionOpen] = useState(false);
  const [activeDialog, setActiveDialog] = useState<"none" | "pr" | "commit">("none");
  const [activeTab, setActiveTab] = useState<"chat" | "changes">("chat");
  const [resuming, setResuming] = useState(false);

  const git = useGitActions(sessionId);

  const totalChangedFiles =
    (git.diffResult?.files.length ?? 0) + (git.uncommittedDiffResult?.files.length ?? 0);
  const hasChanges = totalChangedFiles > 0;
  const totalAdd = (git.diffTotals?.add ?? 0) + (git.uncommittedDiffTotals?.add ?? 0);
  const totalDel = (git.diffTotals?.del ?? 0) + (git.uncommittedDiffTotals?.del ?? 0);

  // Reset transient UI state on session switch
  const prevSessionIdRef = useRef(sessionId);
  useEffect(() => {
    if (prevSessionIdRef.current !== sessionId) {
      prevSessionIdRef.current = sessionId;
      setMobileSessionOpen(false);
      setActiveDialog("none");
      setActiveTab("chat");
      setResuming(false);
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

  // Refresh git status on session navigation (skip if already fresh)
  useEffect(() => {
    if (!isGitFresh(sessionId)) {
      refreshGitStatus(ws, sessionId).catch(() => {});
    }
  }, [ws, sessionId]);

  // Load history on mount or session switch
  const sessionExists = !!meta;
  const hasTurns = turns.length > 0;
  useEffect(() => {
    if (sessionExists && !hasTurns) {
      loadSessionHistory(ws, sessionId);
    }
  }, [ws, sessionId, sessionExists, hasTurns]);

  // Redirect if session was deleted or doesn't exist
  useEffect(() => {
    if (sessionListLoaded && !meta) {
      navigate({
        to: "/project/$projectSlug",
        params: { projectSlug },
        replace: true,
      });
    }
  }, [sessionListLoaded, meta, navigate, projectSlug]);

  const handlePlanModeChange = useCallback(
    (enabled: boolean) => {
      useChatStore.getState().setSessionPlanMode(sessionId, enabled);
      const mode = enabled ? "plan" : "default";
      setPermissionMode(ws, sessionId, mode).catch((err) => {
        toast.error(getErrorMessage(err, "Failed to set plan mode"));
      });
    },
    [ws, sessionId],
  );

  const handleAutoApproveChange = useCallback(
    (enabled: boolean) => {
      useChatStore.getState().setSessionAutoApprove(sessionId, enabled);
      setAutoApprove(ws, sessionId, enabled).catch((err) => {
        toast.error(getErrorMessage(err, "Failed to set auto-approve"));
      });
    },
    [ws, sessionId],
  );

  const handleModelChange = useCallback(
    (model: ModelId) => {
      setSessionModel(ws, sessionId, model).catch((err) => {
        toast.error(getErrorMessage(err, "Failed to set model"));
      });
    },
    [ws, sessionId],
  );

  const handleTextPersist = useCallback(
    (text: string) => {
      useUIStore.getState().setDraft(sessionId, text);
    },
    [sessionId],
  );

  const handleSend = useCallback(
    async (prompt: string, attachments?: Attachment[]) => {
      useUIStore.getState().clearDraft(sessionId);
      if (sessionState === "running") {
        useChatStore.getState().enqueueMessage(sessionId, prompt, attachments);
        return;
      }
      try {
        await submitQuery(ws, sessionId, prompt, attachments);
      } catch (err) {
        const msg = getErrorMessage(err, "Failed to send message");
        toast.error(msg, {
          action: { label: "Copy", onClick: () => copyToClipboard(msg) },
        });
        useChatStore.getState().setSessionState(sessionId, "idle");
      }
    },
    [ws, sessionId, sessionState],
  );

  const handleStartFresh = useCallback(
    async (plan: string) => {
      try {
        const newId = await createSession(ws, projectId, "", !!meta?.worktreeBranch, {
          model: meta?.model,
          autoApprove: meta?.autoApprove,
        });
        await stopSession(ws, sessionId);
        await submitQuery(ws, newId, plan);
        navigate({
          to: "/project/$projectSlug/session/$sessionShortId",
          params: { projectSlug, sessionShortId: sessionShortId(newId) },
        });
      } catch (err) {
        toast.error(getErrorMessage(err, "Failed to start fresh session"));
      }
    },
    [ws, projectId, sessionId, meta, navigate, projectSlug],
  );

  const handleInterrupt = useCallback(async () => {
    if (queuedMessages.length > 0) {
      const text = queuedMessages.map((m) => m.prompt).join("\n\n");
      useChatStore.getState().clearQueue(sessionId);
      composerRef.current?.setText(text);
    }
    interruptSession(ws, sessionId).catch(console.error);
  }, [ws, sessionId, queuedMessages]);

  const handleResume = useCallback(async () => {
    if (resuming) return;
    setResuming(true);
    try {
      await resumeSession(ws, sessionId);
    } catch (err) {
      toast.error(getErrorMessage(err, "Failed to resume session"));
    } finally {
      setResuming(false);
    }
  }, [ws, sessionId, resuming]);

  const isResumable = resumableStates.has(sessionState);

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

  if (!meta) {
    return <StatusPage message="Loading session..." />;
  }

  return (
    <div className="flex h-full" data-project-id={projectId}>
      <div className="flex-1 flex flex-col min-w-0 h-full overflow-hidden">
        <SessionHeader
          meta={meta}
          hasPendingInput={!!pendingApproval || !!pendingQuestion}
          showPanelButton={isMobile && showPanel}
          onOpenPanel={() => setMobileSessionOpen(true)}
        />

        {/* Tab bar — only when there are changes to view */}
        {hasChanges && (
          <div className="shrink-0 flex gap-1 px-2 pt-1.5 pb-0 border-b text-xs">
            <button
              type="button"
              onClick={() => setActiveTab("chat")}
              className={cn(
                "flex items-center gap-1.5 px-3 py-1.5 rounded-t transition-colors",
                activeTab === "chat"
                  ? "text-foreground bg-muted/60 border-b-2 border-foreground"
                  : "text-muted-foreground hover:text-foreground hover:bg-muted/40",
              )}
            >
              <MessageSquare className="h-3.5 w-3.5" />
              Chat
            </button>
            <button
              type="button"
              onClick={() => setActiveTab("changes")}
              className={cn(
                "flex items-center gap-1.5 px-3 py-1.5 rounded-t transition-colors",
                activeTab === "changes"
                  ? "text-foreground bg-muted/60 border-b-2 border-foreground"
                  : "text-muted-foreground hover:text-foreground hover:bg-muted/40",
              )}
            >
              <FileDiff className="h-3.5 w-3.5" />
              Changes
              {(totalAdd > 0 || totalDel > 0) && (
                <span className="flex items-center gap-1 ml-1 text-[11px]">
                  {totalAdd > 0 && <span className="text-success">+{totalAdd}</span>}
                  {totalDel > 0 && <span className="text-destructive">-{totalDel}</span>}
                </span>
              )}
            </button>
          </div>
        )}

        {activeTab === "changes" && hasChanges ? (
          <ChangesView committedDiff={git.diffResult} uncommittedDiff={git.uncommittedDiffResult} />
        ) : (
          <>
            <MessageList
              turns={turns}
              sessionId={sessionId}
              projectId={projectId}
              sessionState={sessionState}
              projectPath={project?.path}
              worktreePath={meta.worktreePath}
              isLoadingHistory={isLoadingHistory}
            />
            {pendingApproval &&
              (pendingApproval.toolName === "ExitPlanMode" ? (
                <PlanReviewBanner
                  sessionId={sessionId}
                  approval={pendingApproval}
                  onStartFresh={handleStartFresh}
                />
              ) : (
                <ApprovalBanner
                  sessionId={sessionId}
                  approval={pendingApproval}
                  projectPath={project?.path}
                  worktreePath={meta.worktreePath}
                />
              ))}
            {pendingQuestion && <QuestionBanner sessionId={sessionId} pending={pendingQuestion} />}

            {queuedMessages.length > 0 && (
              <MessageQueue
                messages={queuedMessages}
                onCancel={(msg) => {
                  useChatStore.getState().cancelQueuedMessage(sessionId, msg.id);
                  composerRef.current?.setText(msg.prompt);
                }}
              />
            )}
            {(contextUsage || compacting) && (
              <ContextBar usage={contextUsage} compacting={compacting} />
            )}
            {isResumable && (
              <ResumeBanner
                state={sessionState as "stopped" | "failed" | "done"}
                onResume={handleResume}
                resuming={resuming}
              />
            )}
            <MessageComposer
              key={sessionId}
              projectId={projectId}
              ref={composerRef}
              onSend={handleSend}
              initialText={draft}
              onTextPersist={handleTextPersist}
              disabled={sessionState === "merging" || compacting}
              isRunning={sessionState === "running"}
              onInterrupt={handleInterrupt}
              placeholder={
                compacting
                  ? "Compacting context..."
                  : sessionState === "merging"
                    ? "Git operation in progress..."
                    : resumePlaceholders[sessionState]
              }
              worktree={isWorktree}
              planMode={planMode}
              onPlanModeChange={handlePlanModeChange}
              autoApprove={autoApprove}
              onAutoApproveChange={handleAutoApproveChange}
              model={(meta.model as ModelId) ?? undefined}
              onModelChange={handleModelChange}
              effort={(meta.effort as EffortLevel) ?? ""}
              onEmptySubmit={isResumable ? handleResume : undefined}
            />
          </>
        )}
      </div>

      {/* Right panel — git + todos */}
      {showPanel &&
        (isMobile ? (
          <Sheet open={mobileSessionOpen} onOpenChange={setMobileSessionOpen}>
            <SheetContent side="right" className="p-0" showCloseButton={false}>
              <SheetTitle className="sr-only">Session details</SheetTitle>
              <SheetDescription className="sr-only">Git status and todos</SheetDescription>
              <SessionPanel
                meta={meta}
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
            meta={meta}
            todos={todos}
            onExpand={() => setPanelCollapsed(false)}
          />
        ) : (
          <div className="w-72 border-l shrink-0">
            <SessionPanel
              meta={meta}
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
        defaultTitle={meta.name}
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
        defaultTitle={meta.name}
        onSubmit={async (message) => {
          const ok = await git.handleCommit(message);
          if (ok) setActiveDialog("none");
        }}
        loading={git.committing}
      />
    </div>
  );
}
