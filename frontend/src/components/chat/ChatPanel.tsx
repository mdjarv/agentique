import { MessageComposer } from "~/components/chat/MessageComposer";
import { MessageList } from "~/components/chat/MessageList";
import { SessionHeader } from "~/components/chat/SessionHeader";
import { useChatSession } from "~/hooks/useChatSession";
import { useAppStore } from "~/stores/app-store";
import { useChatStore } from "~/stores/chat-store";

interface ChatPanelProps {
  projectId: string;
}

export function ChatPanel({ projectId }: ChatPanelProps) {
  const { sendQuery } = useChatSession(projectId);
  const project = useAppStore((s) => s.projects.find((p) => p.id === projectId));
  const activeSession = useChatStore((s) =>
    s.activeSessionId ? s.sessions[s.activeSessionId] : undefined,
  );

  const sessionState = activeSession?.meta.state ?? "disconnected";
  const isDraft = sessionState === "draft";
  const worktree = activeSession?.meta.worktree ?? false;

  return (
    <div className="flex flex-col h-full" data-project-id={projectId}>
      {activeSession && activeSession.meta.state !== "draft" && (
        <SessionHeader session={activeSession} />
      )}
      <MessageList
        turns={activeSession?.turns ?? []}
        currentAssistantText={activeSession?.currentAssistantText ?? ""}
        sessionState={sessionState}
        projectPath={project?.path}
        worktreePath={activeSession?.meta.worktreePath}
      />
      <MessageComposer
        onSend={sendQuery}
        disabled={sessionState === "running"}
        isDraft={isDraft}
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
