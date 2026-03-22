import { MessageComposer } from "~/components/chat/MessageComposer";
import { MessageList } from "~/components/chat/MessageList";
import { SessionTabs } from "~/components/chat/SessionTabs";
import { useChatSession } from "~/hooks/useChatSession";
import { useChatStore } from "~/stores/chat-store";

interface ChatPanelProps {
  projectId: string;
}

export function ChatPanel({ projectId }: ChatPanelProps) {
  const { sendQuery } = useChatSession(projectId);
  const sessionState = useChatStore((s) => s.sessionState);

  return (
    <div className="flex flex-col h-full" data-project-id={projectId}>
      <SessionTabs state={sessionState} />
      <MessageList />
      <MessageComposer onSend={sendQuery} disabled={sessionState === "running"} />
    </div>
  );
}
