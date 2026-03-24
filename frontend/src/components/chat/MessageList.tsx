import { Loader2 } from "lucide-react";
import { useEffect, useRef } from "react";
import { TurnBlock } from "~/components/chat/TurnBlock";
import type { Turn } from "~/stores/chat-store";

interface MessageListProps {
  turns: Turn[];
  sessionId: string;
  currentAssistantText: string;
  sessionState: string;
  projectPath?: string;
  worktreePath?: string;
  isLoadingHistory?: boolean;
}

export function MessageList({
  turns,
  sessionId,
  currentAssistantText,
  sessionState,
  projectPath,
  worktreePath,
  isLoadingHistory,
}: MessageListProps) {
  const scrollRef = useRef<HTMLDivElement>(null);
  const bottomRef = useRef<HTMLDivElement>(null);

  // biome-ignore lint/correctness/useExhaustiveDependencies: scroll on every content change
  useEffect(() => {
    bottomRef.current?.scrollIntoView({ behavior: "smooth" });
  }, [turns, currentAssistantText]);

  if (turns.length === 0) {
    if (isLoadingHistory) {
      return (
        <div className="flex-1 flex items-center justify-center">
          <Loader2 className="h-5 w-5 animate-spin text-muted-foreground" />
        </div>
      );
    }
    return (
      <div className="flex-1 flex items-center justify-center">
        <p className="text-muted-foreground">Send a message to start chatting</p>
      </div>
    );
  }

  return (
    <div ref={scrollRef} className="flex-1 overflow-y-auto overflow-x-hidden min-h-0">
      <div className="p-4 space-y-6 min-w-0">
        {turns.map((turn, i) => (
          <TurnBlock
            key={turn.id}
            turn={turn}
            isLast={i === turns.length - 1}
            sessionId={sessionId}
            currentAssistantText={currentAssistantText}
            sessionState={sessionState}
            projectPath={projectPath}
            worktreePath={worktreePath}
          />
        ))}
        <div ref={bottomRef} />
      </div>
    </div>
  );
}
