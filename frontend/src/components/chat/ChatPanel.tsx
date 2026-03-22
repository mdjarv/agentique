import { MessageComposer } from "~/components/chat/MessageComposer";
import { MessageList } from "~/components/chat/MessageList";
import { useChatSession } from "~/hooks/useChatSession";
import { useChatStore } from "~/stores/chat-store";

interface ChatPanelProps {
  projectId: string;
}

export function ChatPanel({ projectId }: ChatPanelProps) {
  const { sendQuery } = useChatSession(projectId);
  const activeSession = useChatStore((s) =>
    s.activeSessionId ? s.sessions[s.activeSessionId] : undefined,
  );

  const sessionState = activeSession?.meta.state ?? "disconnected";
  const isDraft = sessionState === "draft";
  const worktree = activeSession?.meta.worktree ?? false;

  return (
    <div className="flex flex-col h-full" data-project-id={projectId}>
      <MessageList
        turns={activeSession?.turns ?? []}
        currentAssistantText={activeSession?.currentAssistantText ?? ""}
        sessionState={sessionState}
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
