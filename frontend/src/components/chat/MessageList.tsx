import { useEffect, useRef } from "react";
import { TurnBlock } from "~/components/chat/TurnBlock";
import { ScrollArea } from "~/components/ui/scroll-area";
import type { Turn } from "~/stores/chat-store";

interface MessageListProps {
  turns: Turn[];
  currentAssistantText: string;
  sessionState: string;
}

export function MessageList({ turns, currentAssistantText, sessionState }: MessageListProps) {
  const bottomRef = useRef<HTMLDivElement>(null);

  // biome-ignore lint/correctness/useExhaustiveDependencies: scroll on every content change
  useEffect(() => {
    bottomRef.current?.scrollIntoView({ behavior: "smooth" });
  }, [turns, currentAssistantText]);

  if (turns.length === 0) {
    return (
      <div className="flex-1 flex items-center justify-center">
        <p className="text-muted-foreground">Send a message to start chatting</p>
      </div>
    );
  }

  return (
    <ScrollArea className="flex-1">
      <div className="p-4 space-y-6">
        {turns.map((turn, i) => (
          <TurnBlock
            key={turn.id}
            turn={turn}
            isLast={i === turns.length - 1}
            currentAssistantText={currentAssistantText}
            sessionState={sessionState}
          />
        ))}
        <div ref={bottomRef} />
      </div>
    </ScrollArea>
  );
}
