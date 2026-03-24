import { ArrowDown, Loader2 } from "lucide-react";
import { useCallback, useEffect, useRef, useState } from "react";
import { TurnBlock } from "~/components/chat/TurnBlock";
import { Button } from "~/components/ui/button";
import type { Turn } from "~/stores/chat-store";

const SCROLL_THRESHOLD = 48;

function isNearBottom(el: HTMLElement): boolean {
  return el.scrollHeight - el.scrollTop - el.clientHeight < SCROLL_THRESHOLD;
}

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
  const [following, setFollowing] = useState(true);

  const handleScroll = useCallback(() => {
    const el = scrollRef.current;
    if (!el) return;
    setFollowing(isNearBottom(el));
  }, []);

  // biome-ignore lint/correctness/useExhaustiveDependencies: scroll on every content change
  useEffect(() => {
    if (following) {
      bottomRef.current?.scrollIntoView({ behavior: "smooth" });
    }
  }, [turns, currentAssistantText, following]);

  // Reset to following when switching sessions
  // biome-ignore lint/correctness/useExhaustiveDependencies: reset on session change
  useEffect(() => {
    setFollowing(true);
  }, [sessionId]);

  const scrollToBottom = useCallback(() => {
    bottomRef.current?.scrollIntoView({ behavior: "smooth" });
    setFollowing(true);
  }, []);

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
    <div
      ref={scrollRef}
      onScroll={handleScroll}
      className="flex-1 overflow-y-auto overflow-x-hidden min-h-0 relative"
    >
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
      {!following && (
        <Button
          variant="secondary"
          size="icon"
          onClick={scrollToBottom}
          className="sticky bottom-4 left-1/2 -translate-x-1/2 rounded-full shadow-lg z-10 opacity-80 hover:opacity-100 transition-opacity"
        >
          <ArrowDown className="h-4 w-4" />
        </Button>
      )}
    </div>
  );
}
