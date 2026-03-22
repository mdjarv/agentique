import { useState } from "react";
import { MessageComposer } from "~/components/chat/MessageComposer";
import { MessageList } from "~/components/chat/MessageList";
import { NewSessionDialog } from "~/components/chat/NewSessionDialog";
import { SessionTabs } from "~/components/chat/SessionTabs";
import { useChatSession } from "~/hooks/useChatSession";
import { useChatStore } from "~/stores/chat-store";

interface ChatPanelProps {
  projectId: string;
}

export function ChatPanel({ projectId }: ChatPanelProps) {
  const { sendQuery, createSession, stopSession } = useChatSession(projectId);
  const activeSession = useChatStore((s) =>
    s.activeSessionId ? s.sessions[s.activeSessionId] : undefined,
  );
  const [showNewSession, setShowNewSession] = useState(false);

  const sessionState = activeSession?.meta.state ?? "disconnected";

  return (
    <div className="flex flex-col h-full" data-project-id={projectId}>
      <SessionTabs onCreateSession={() => setShowNewSession(true)} onStopSession={stopSession} />
      <MessageList
        turns={activeSession?.turns ?? []}
        currentAssistantText={activeSession?.currentAssistantText ?? ""}
        sessionState={sessionState}
      />
      <MessageComposer onSend={sendQuery} disabled={sessionState === "running"} />
      <NewSessionDialog
        open={showNewSession}
        onOpenChange={setShowNewSession}
        onSubmit={async (name, worktree, branch) => {
          await createSession(name, worktree, branch);
          setShowNewSession(false);
        }}
      />
    </div>
  );
}
