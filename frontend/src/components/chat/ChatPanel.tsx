import { MessageComposer } from "~/components/chat/MessageComposer";
import { MessageList } from "~/components/chat/MessageList";
import { SessionTabs } from "~/components/chat/SessionTabs";

interface ChatPanelProps {
  projectId: string;
}

export function ChatPanel({ projectId }: ChatPanelProps) {
  return (
    <div className="flex flex-col h-full" data-project-id={projectId}>
      <SessionTabs />
      <MessageList />
      <MessageComposer />
    </div>
  );
}
