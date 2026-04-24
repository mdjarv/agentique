import { useNavigate } from "@tanstack/react-router";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { toast } from "sonner";
import { useShallow } from "zustand/shallow";
import { ApprovalBanner } from "~/components/chat/banners/ApprovalBanner";
import { PlanReviewBanner } from "~/components/chat/banners/PlanReviewBanner";
import { QuestionBanner } from "~/components/chat/banners/QuestionBanner";
import { ResumeBanner } from "~/components/chat/banners/ResumeBanner";
import { SpawnWorkerApprovalBanner } from "~/components/chat/banners/SpawnWorkerApprovalBanner";
import { ContextBar } from "~/components/chat/ContextBar";
import { ChangesView } from "~/components/chat/changes/ChangesView";
import { CommitDialog } from "~/components/chat/dialogs/CommitDialog";
import { CreatePRDialog } from "~/components/chat/dialogs/CreatePRDialog";
import { type ComposerHandle, MessageComposer } from "~/components/chat/MessageComposer";
import { MessageList } from "~/components/chat/MessageList";
import { SessionHeader } from "~/components/chat/SessionHeader";
import { SessionTabBar } from "~/components/chat/SessionTabBar";
import { TodosView } from "~/components/chat/TodosView";
import { StatusPage } from "~/components/layout/PageHeader";
import { useGitActions } from "~/hooks/git/useGitActions";
import { useProjectGitActions } from "~/hooks/git/useProjectGitActions";
import { useSessionState } from "~/hooks/session/useSessionState";
import { useIsMobile } from "~/hooks/useIsMobile";
import { useTheme } from "~/hooks/useTheme";
import { useWebSocket } from "~/hooks/useWebSocket";
import type { EffortLevel } from "~/lib/composer-constants";
import { getProjectColor } from "~/lib/project-colors";
import {
  createSession,
  enqueueMessage,
  interruptSession,
  isGitFresh,
  type ModelId,
  refreshGitStatus,
  resumeSession,
  setAutoApproveMode,
  setPermissionMode,
  setSessionModel,
  stopSession,
} from "~/lib/session/actions";
import { loadSessionHistory } from "~/lib/session/history";
import { copyToClipboard, getErrorMessage, sessionShortId } from "~/lib/utils";
import { useAppStore } from "~/stores/app-store";
import type { Attachment, AutoApproveMode, PendingApproval } from "~/stores/chat-store";
import { useChatStore } from "~/stores/chat-store";

function ApprovalBannerSwitch({
  sessionId,
  approval,
  onStartFresh,
  projectPath,
  worktreePath,
}: {
  sessionId: string;
  approval: PendingApproval;
  onStartFresh: (plan: string) => void;
  projectPath?: string;
  worktreePath?: string;
}) {
  if (approval.toolName === "ExitPlanMode") {
    return (
      <PlanReviewBanner sessionId={sessionId} approval={approval} onStartFresh={onStartFresh} />
    );
  }
  if (approval.toolName === "SpawnWorkers") {
    return <SpawnWorkerApprovalBanner sessionId={sessionId} approval={approval} />;
  }
  return (
    <ApprovalBanner
      sessionId={sessionId}
      approval={approval}
      projectPath={projectPath}
      worktreePath={worktreePath}
    />
  );
}

import { useUIStore } from "~/stores/ui-store";

export type SessionTab = "chat" | "todos" | "git" | "changes"; // "git" kept for backward compat URLs

interface ChatPanelProps {
  projectId: string;
  sessionId: string;
  tab?: SessionTab;
  onTabChange?: (tab: SessionTab) => void;
}

const resumePlaceholders: Record<string, string> = {
  stopped: "Send a message or press Enter to resume...",
  done: "Send a message or press Enter to continue...",
  failed: "Send a message or press Enter to retry...",
};

const resumableStates = new Set(["stopped", "failed", "done"]);

export function ChatPanel({ projectId, sessionId, tab, onTabChange }: ChatPanelProps) {
  const navigate = useNavigate();
  const ws = useWebSocket();
  const project = useAppStore((s) => s.projects.find((p) => p.id === projectId));
  const projectSlug = project?.slug ?? "";
  const mainBranch = useAppStore((s) => s.projectGitStatus[projectId]?.branch);
  const projectGitStatus = useAppStore((s) => s.projectGitStatus[projectId]);
  const projectGitActions = useProjectGitActions(projectId);

  const {
    turns,
    meta,
    pendingApproval,
    pendingQuestion,
    planMode,
    autoApproveMode,
    todos,
    contextUsage,
    compacting,
  } = useSessionState(sessionId);
  const sessionListLoaded = useChatStore((s) => s.loadedProjects.has(projectId));
  const isLoadingHistory = useChatStore((s) => s.historyLoading.has(sessionId));

  const projectIds = useAppStore(useShallow((s) => s.projects.map((p) => p.id)));
  const { resolvedTheme } = useTheme();
  const agentColor = useMemo(
    () =>
      project
        ? getProjectColor(project.color, project.id, projectIds, resolvedTheme).fg
        : undefined,
    [project, projectIds, resolvedTheme],
  );

  const composerRef = useRef<ComposerHandle>(null);
  const sessionState = meta?.state ?? "idle";
  const draft = useUIStore((s) => s.drafts[sessionId] ?? "");
  const stashStack = useUIStore((s) => s.stashes[sessionId]);
  const stashedText = stashStack?.[stashStack.length - 1] ?? "";
  const stashDepth = stashStack?.length ?? 0;
  const hasTodos = todos !== null && todos.length > 0;
  const isWorktree = !!meta?.worktreeBranch;
  const isDirty = meta?.hasUncommitted || meta?.hasDirtyWorktree;
  const hasRemoteChanges =
    (projectGitStatus?.aheadRemote ?? 0) > 0 || (projectGitStatus?.behindRemote ?? 0) > 0;

  const [activeDialog, setActiveDialog] = useState<"none" | "pr" | "commit">("none");
  const activeTab: SessionTab = tab === "git" ? "changes" : (tab ?? "chat");
  const setActiveTab = useCallback((t: SessionTab) => onTabChange?.(t), [onTabChange]);
  const [resuming, setResuming] = useState(false);
  const [expandFile, setExpandFile] = useState<string | null>(null);

  const handleExpandFileConsumed = useCallback(() => {
    setExpandFile(null);
  }, []);

  const git = useGitActions(sessionId);

  const hasChanges =
    (git.diffResult?.files.length ?? 0) + (git.uncommittedDiffResult?.files.length ?? 0) > 0;
  const totalAdd = (git.diffTotals?.add ?? 0) + (git.uncommittedDiffTotals?.add ?? 0);
  const totalDel = (git.diffTotals?.del ?? 0) + (git.uncommittedDiffTotals?.del ?? 0);

  // Reset transient UI state on session switch
  const prevSessionIdRef = useRef(sessionId);
  useEffect(() => {
    if (prevSessionIdRef.current !== sessionId) {
      prevSessionIdRef.current = sessionId;
      setActiveDialog("none");
      setResuming(false);
    }
  }, [sessionId]);

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
      refreshGitStatus(ws, sessionId).catch((err) => console.error("refreshGitStatus failed", err));
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

  const handleAutoApproveModeChange = useCallback(
    (mode: AutoApproveMode) => {
      useChatStore.getState().setSessionAutoApproveMode(sessionId, mode);
      setAutoApproveMode(ws, sessionId, mode).catch((err) => {
        toast.error(getErrorMessage(err, "Failed to set auto-approve mode"));
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

  const handleStash = useCallback(
    (text: string) => {
      useUIStore.getState().pushStash(sessionId, text);
      useUIStore.getState().clearDraft(sessionId);
      toast("Prompt stashed", { description: "Ctrl+S on empty input to restore" });
    },
    [sessionId],
  );

  const handleUnstash = useCallback((): string | undefined => {
    return useUIStore.getState().popStash(sessionId);
  }, [sessionId]);

  const handleSend = useCallback(
    async (prompt: string, attachments?: Attachment[]) => {
      useUIStore.getState().clearDraft(sessionId);
      try {
        await enqueueMessage(ws, sessionId, prompt, attachments);
        setActiveTab("chat");
        const popped = useUIStore.getState().popStash(sessionId);
        if (popped) {
          composerRef.current?.setText(popped);
        }
      } catch (err) {
        const msg = getErrorMessage(err, "Failed to send message");
        toast.error(msg, {
          action: { label: "Copy", onClick: () => copyToClipboard(msg) },
        });
      }
    },
    [ws, sessionId, setActiveTab],
  );

  const handleStartFresh = useCallback(
    async (plan: string) => {
      try {
        const newId = await createSession(ws, projectId, "", !!meta?.worktreeBranch, {
          model: meta?.model,
          autoApproveMode: meta?.autoApproveMode,
        });
        await stopSession(ws, sessionId);
        await enqueueMessage(ws, newId, plan);
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
    interruptSession(ws, sessionId).catch(console.error);
  }, [ws, sessionId]);

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
  const isMobile = useIsMobile();

  if (!meta) {
    return <StatusPage message="Loading session..." />;
  }
  const uncommittedCount = git.uncommittedFiles?.length ?? 0;
  const hasGitContent = isWorktree || isDirty || hasRemoteChanges || hasChanges;
  const showTabs = hasTodos || hasGitContent || hasChanges;
  const ahead = isWorktree ? (meta?.commitsAhead ?? 0) : (projectGitStatus?.aheadRemote ?? 0);
  const behind = isWorktree ? (meta?.commitsBehind ?? 0) : (projectGitStatus?.behindRemote ?? 0);

  const tabBarElement = showTabs ? (
    <SessionTabBar
      activeTab={activeTab}
      onTabChange={setActiveTab}
      hasTodos={hasTodos}
      todosCompleted={todos?.filter((t) => t.status === "completed").length}
      todosTotal={todos?.length}
      hasGitContent={hasGitContent}
      ahead={ahead}
      behind={behind}
      uncommittedCount={uncommittedCount}
      hasChanges={hasChanges}
      totalAdd={totalAdd}
      totalDel={totalDel}
      accentColor={agentColor}
    />
  ) : null;

  return (
    <div
      className="flex flex-col h-full chat-frost"
      data-project-id={projectId}
      style={
        agentColor
          ? ({ "--agent": agentColor, background: `${agentColor}08` } as React.CSSProperties)
          : undefined
      }
    >
      <SessionHeader
        meta={meta}
        hasPendingInput={!!pendingApproval || !!pendingQuestion}
        tabBar={tabBarElement}
        accentColor={agentColor}
        git={git}
        projectGitStatus={projectGitStatus}
      />

      {/* Tab bar — mobile only (desktop renders inline in header) */}
      {isMobile && showTabs && (
        <div className="shrink-0 flex gap-1 px-2 py-1 border-b text-xs">{tabBarElement}</div>
      )}

      {/* Tab content */}
      {activeTab === "todos" && hasTodos ? (
        <TodosView todos={todos} />
      ) : activeTab === "changes" && hasGitContent ? (
        <ChangesView
          meta={meta}
          git={git}
          mainBranch={mainBranch}
          projectGitStatus={projectGitStatus}
          projectGitActions={projectGitActions}
          committedDiff={git.diffResult}
          uncommittedDiff={git.uncommittedDiffResult}
          sessionState={sessionState}
          onSendMessage={handleSend}
          onOpenDialog={(d: "pr" | "commit") => setActiveDialog(d)}
          expandFile={expandFile}
          onExpandFileConsumed={handleExpandFileConsumed}
        />
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
          {pendingApproval && (
            <ApprovalBannerSwitch
              sessionId={sessionId}
              approval={pendingApproval}
              onStartFresh={handleStartFresh}
              projectPath={project?.path}
              worktreePath={meta.worktreePath}
            />
          )}
          {pendingQuestion && <QuestionBanner sessionId={sessionId} pending={pendingQuestion} />}

          {(contextUsage || compacting) && (
            <ContextBar usage={contextUsage} compacting={compacting} />
          )}
          {isResumable && (
            <ResumeBanner
              state={sessionState as "stopped" | "failed" | "done"}
              onResume={handleResume}
              resuming={resuming}
              branchMissing={meta?.branchMissing}
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
            autoApproveMode={autoApproveMode}
            onAutoApproveModeChange={handleAutoApproveModeChange}
            model={(meta.model as ModelId) ?? undefined}
            onModelChange={handleModelChange}
            effort={(meta.effort as EffortLevel) ?? ""}
            onEmptySubmit={isResumable ? handleResume : undefined}
            stashedText={stashedText || undefined}
            stashDepth={stashDepth}
            onStash={handleStash}
            onUnstash={handleUnstash}
          />
        </>
      )}

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
