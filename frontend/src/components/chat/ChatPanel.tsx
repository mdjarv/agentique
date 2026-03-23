import { useNavigate } from "@tanstack/react-router";
import { useCallback, useEffect } from "react";
import { toast } from "sonner";
import { ApprovalBanner } from "~/components/chat/ApprovalBanner";
import { MessageComposer } from "~/components/chat/MessageComposer";
import { MessageList } from "~/components/chat/MessageList";
import { QuestionBanner } from "~/components/chat/QuestionBanner";
import { SessionHeader } from "~/components/chat/SessionHeader";
import { useChatSession } from "~/hooks/useChatSession";
import { useWebSocket } from "~/hooks/useWebSocket";
import { setAutoApprove, setPermissionMode } from "~/lib/session-actions";
import { useAppStore } from "~/stores/app-store";
import { useChatStore } from "~/stores/chat-store";
import { selectActiveSession } from "~/stores/selectors";
import { useStreamingStore } from "~/stores/streaming-store";

interface ChatPanelProps {
  projectId: string;
  initialSessionId?: string;
}

const resumePlaceholders: Record<string, string> = {
  stopped: "Session stopped — send a message to resume...",
  done: "Session complete — send a message to continue...",
  failed: "Session failed — send a message to retry...",
};

export function ChatPanel({ projectId, initialSessionId }: ChatPanelProps) {
  const { sendQuery, interruptSession, loadHistory } = useChatSession(projectId, initialSessionId);
  const navigate = useNavigate();
  const project = useAppStore((s) => s.projects.find((p) => p.id === projectId));
  const activeSessionId = useChatStore((s) => s.activeSessionId);
  const activeSession = useChatStore(selectActiveSession);
  const isLoadingHistory = useChatStore((s) =>
    s.activeSessionId ? s.historyLoading.has(s.activeSessionId) : false,
  );
  const currentAssistantText = useStreamingStore((s) =>
    activeSessionId ? (s.texts[activeSessionId] ?? "") : "",
  );

  const ws = useWebSocket();
  const sessionState = activeSession?.meta.state ?? "disconnected";
  const isDraft = sessionState === "draft";
  const planMode = activeSession?.planMode ?? false;
  const autoApprove = activeSession?.autoApprove ?? false;

  const handlePlanModeChange = useCallback(
    (enabled: boolean) => {
      if (!activeSessionId) return;
      useChatStore.getState().setSessionPlanMode(activeSessionId, enabled);
      if (!isDraft) {
        const mode = enabled ? "plan" : "default";
        setPermissionMode(ws, activeSessionId, mode).catch((err) => {
          toast.error(err instanceof Error ? err.message : "Failed to set plan mode");
        });
      }
    },
    [ws, activeSessionId, isDraft],
  );

  const handleAutoApproveChange = useCallback(
    (enabled: boolean) => {
      if (!activeSessionId) return;
      useChatStore.getState().setSessionAutoApprove(activeSessionId, enabled);
      if (!isDraft) {
        setAutoApprove(ws, activeSessionId, enabled).catch((err) => {
          toast.error(err instanceof Error ? err.message : "Failed to set auto-approve");
        });
      }
    },
    [ws, activeSessionId, isDraft],
  );

  // Load history when switching to a session that hasn't been loaded yet
  useEffect(() => {
    if (!activeSessionId) return;
    const s = useChatStore.getState().sessions[activeSessionId];
    if (s && s.turns.length === 0 && s.meta.state !== "draft") {
      loadHistory(activeSessionId);
    }
  }, [activeSessionId, loadHistory]);

  // Sync active session ID to URL search param
  useEffect(() => {
    const isDraftSession = activeSessionId?.startsWith("draft-");
    const session = isDraftSession || !activeSessionId ? undefined : activeSessionId;
    navigate({
      to: "/project/$projectId",
      params: { projectId },
      search: { session },
      replace: true,
    });
  }, [activeSessionId, navigate, projectId]);

  const resumePlaceholder = resumePlaceholders[sessionState];
  const worktree = activeSession?.meta.worktree ?? false;

  if (!activeSession) {
    return (
      <div
        className="flex flex-col h-full items-center justify-center text-muted-foreground"
        data-project-id={projectId}
      >
        <p className="text-sm">Select a session or start a new chat</p>
      </div>
    );
  }

  return (
    <div className="flex flex-col h-full" data-project-id={projectId}>
      {activeSession.meta.state !== "draft" && <SessionHeader session={activeSession} />}
      <MessageList
        turns={activeSession?.turns ?? []}
        currentAssistantText={currentAssistantText}
        sessionState={sessionState}
        projectPath={project?.path}
        worktreePath={activeSession?.meta.worktreePath}
        isLoadingHistory={isLoadingHistory}
      />
      {activeSession?.pendingApproval && activeSessionId && (
        <ApprovalBanner
          sessionId={activeSessionId}
          approval={activeSession.pendingApproval}
          projectPath={project?.path}
          worktreePath={activeSession?.meta.worktreePath}
        />
      )}
      {activeSession?.pendingQuestion && activeSessionId && (
        <QuestionBanner sessionId={activeSessionId} pending={activeSession.pendingQuestion} />
      )}
      <MessageComposer
        onSend={sendQuery}
        disabled={sessionState === "running"}
        isRunning={sessionState === "running"}
        onInterrupt={() => {
          if (activeSessionId) interruptSession(activeSessionId);
        }}
        isDraft={isDraft}
        placeholder={resumePlaceholder}
        planMode={planMode}
        onPlanModeChange={handlePlanModeChange}
        autoApprove={autoApprove}
        onAutoApproveChange={handleAutoApproveChange}
        worktree={worktree}
        onWorktreeChange={(v) => {
          if (activeSession) {
            useChatStore.getState().setDraftWorktree(activeSession.meta.id, v);
          }
        }}
      />
    </div>
  );
}
