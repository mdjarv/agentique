import { ArrowDown, Loader2, Wrench } from "lucide-react";
import { type ReactNode, useCallback, useEffect, useRef, useState } from "react";
import { TurnBlock } from "~/components/chat/TurnBlock";
import { Button } from "~/components/ui/button";
import { Tooltip, TooltipContent, TooltipTrigger } from "~/components/ui/tooltip";
import type { Turn } from "~/stores/chat-store";

const SCROLL_THRESHOLD = 48;
const EAGER_TURN_COUNT = 6;

function isNearBottom(el: HTMLElement): boolean {
  return el.scrollHeight - el.scrollTop - el.clientHeight < SCROLL_THRESHOLD;
}

/** Defers rendering of children until the element scrolls near the viewport. */
function LazyTurn({
  children,
  scrollRoot,
}: {
  children: ReactNode;
  scrollRoot: React.RefObject<HTMLDivElement | null>;
}) {
  const ref = useRef<HTMLDivElement>(null);
  const [visible, setVisible] = useState(false);

  useEffect(() => {
    if (visible) return;
    const el = ref.current;
    const root = scrollRoot.current;
    if (!el || !root) return;
    const observer = new IntersectionObserver(
      (entries) => {
        if (entries[0]?.isIntersecting) {
          setVisible(true);
          observer.disconnect();
        }
      },
      { root, rootMargin: "400px" },
    );
    observer.observe(el);
    return () => observer.disconnect();
  }, [visible, scrollRoot]);

  if (!visible) return <div ref={ref} className="min-h-[4rem]" />;
  return <>{children}</>;
}

interface MessageListProps {
  turns: Turn[];
  sessionId: string;
  projectId: string;
  currentAssistantText: string;
  sessionState: string;
  projectPath?: string;
  worktreePath?: string;
  isLoadingHistory?: boolean;
}

export function MessageList({
  turns,
  sessionId,
  projectId,
  currentAssistantText,
  sessionState,
  projectPath,
  worktreePath,
  isLoadingHistory,
}: MessageListProps) {
  const scrollRef = useRef<HTMLDivElement>(null);
  const bottomRef = useRef<HTMLDivElement>(null);
  const scrollBehaviorRef = useRef<ScrollBehavior>("instant");
  const [following, setFollowing] = useState(true);
  const [showEvents, setShowEvents] = useState(true);

  const handleScroll = useCallback(() => {
    const el = scrollRef.current;
    if (!el) return;
    setFollowing(isNearBottom(el));
  }, []);

  // biome-ignore lint/correctness/useExhaustiveDependencies: scroll on every content change
  useEffect(() => {
    if (following) {
      const behavior = scrollBehaviorRef.current;
      bottomRef.current?.scrollIntoView({ behavior });
      scrollBehaviorRef.current = "smooth";
    }
  }, [turns, currentAssistantText, following]);

  // Reset to following when switching sessions — instant jump, no smooth scroll
  // biome-ignore lint/correctness/useExhaustiveDependencies: reset on session change
  useEffect(() => {
    scrollBehaviorRef.current = "instant";
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
      <div className="p-4 space-y-8 min-w-0">
        {turns.map((turn, i) => {
          const eager = i >= turns.length - EAGER_TURN_COUNT;
          const block = (
            <TurnBlock
              key={turn.id}
              turn={turn}
              isLast={i === turns.length - 1}
              sessionId={sessionId}
              projectId={projectId}
              currentAssistantText={currentAssistantText}
              sessionState={sessionState}
              projectPath={projectPath}
              worktreePath={worktreePath}
              showEvents={showEvents}
            />
          );
          if (eager) return block;
          return (
            <LazyTurn key={turn.id} scrollRoot={scrollRef}>
              {block}
            </LazyTurn>
          );
        })}
        <div ref={bottomRef} />
      </div>
      <div className="sticky bottom-3 right-3 z-10 flex justify-end gap-1.5 pr-3">
        <Tooltip>
          <TooltipTrigger asChild>
            <Button
              variant="secondary"
              size="icon"
              onClick={() => setShowEvents((v) => !v)}
              className="rounded-full shadow-lg opacity-60 hover:opacity-100 transition-opacity h-7 w-7"
            >
              <Wrench className={`h-3.5 w-3.5 ${showEvents ? "" : "text-muted-foreground"}`} />
            </Button>
          </TooltipTrigger>
          <TooltipContent>{showEvents ? "Hide tool events" : "Show tool events"}</TooltipContent>
        </Tooltip>
        {!following && (
          <Tooltip>
            <TooltipTrigger asChild>
              <Button
                variant="secondary"
                size="icon"
                onClick={scrollToBottom}
                className="rounded-full shadow-lg opacity-60 hover:opacity-100 transition-opacity h-7 w-7"
              >
                <ArrowDown className="h-3.5 w-3.5" />
              </Button>
            </TooltipTrigger>
            <TooltipContent>Scroll to bottom</TooltipContent>
          </Tooltip>
        )}
      </div>
    </div>
  );
}
