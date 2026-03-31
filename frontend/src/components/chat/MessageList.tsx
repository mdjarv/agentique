import { ArrowDown, Loader2, Wrench } from "lucide-react";
import { type ReactNode, memo, useCallback, useEffect, useMemo, useRef, useState } from "react";
import { TurnBlock } from "~/components/chat/TurnBlock";
import { UserMessage } from "~/components/chat/UserMessage";
import { Button } from "~/components/ui/button";
import { Tooltip, TooltipContent, TooltipTrigger } from "~/components/ui/tooltip";
import type { ChatEvent, Turn } from "~/stores/chat-store";
import { useStreamingStore } from "~/stores/streaming-store";

const SCROLL_THRESHOLD = 48;
const EAGER_TURN_COUNT = 6;
const EMPTY_PENDING: ChatEvent[] = [];

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

/** Manages auto-scroll: instant during streaming, smooth on new turns. */
const ScrollAnchor = memo(function ScrollAnchor({
  sessionId,
  turns,
  following,
}: {
  sessionId: string;
  turns: Turn[];
  following: boolean;
}) {
  const bottomRef = useRef<HTMLDivElement>(null);
  const streamingLen = useStreamingStore((s) => s.texts[sessionId]?.length ?? 0);
  const isStreaming = streamingLen > 0;
  const prevStreamingRef = useRef(isStreaming);
  const scrollBehaviorRef = useRef<ScrollBehavior>("instant");
  const rafRef = useRef<number>(0);

  // biome-ignore lint/correctness/useExhaustiveDependencies: scroll on every content change
  useEffect(() => {
    if (!following) return;
    const wasStreaming = prevStreamingRef.current;
    prevStreamingRef.current = isStreaming;
    const behavior: ScrollBehavior =
      isStreaming || wasStreaming ? "instant" : scrollBehaviorRef.current;
    scrollBehaviorRef.current = "smooth";
    cancelAnimationFrame(rafRef.current);
    rafRef.current = requestAnimationFrame(() => {
      bottomRef.current?.scrollIntoView({ behavior });
    });
  }, [turns, streamingLen, following]);

  useEffect(() => () => cancelAnimationFrame(rafRef.current), []);

  // Reset to instant on session switch
  // biome-ignore lint/correctness/useExhaustiveDependencies: reset on session change
  useEffect(() => {
    scrollBehaviorRef.current = "instant";
    prevStreamingRef.current = false;
  }, [sessionId]);

  return <div ref={bottomRef} />;
});

interface MessageListProps {
  turns: Turn[];
  sessionId: string;
  projectId: string;
  sessionState: string;
  projectPath?: string;
  worktreePath?: string;
  isLoadingHistory?: boolean;
}

export function MessageList({
  turns,
  sessionId,
  projectId,
  sessionState,
  projectPath,
  worktreePath,
  isLoadingHistory,
}: MessageListProps) {
  const scrollRef = useRef<HTMLDivElement>(null);
  const bottomRef = useRef<HTMLDivElement>(null);
  const [following, setFollowing] = useState(true);
  const [showEvents, setShowEvents] = useState(true);

  const pendingMessages = useMemo(() => {
    const last = turns[turns.length - 1];
    if (!last || last.complete) return EMPTY_PENDING;
    return last.events.filter((e) => e.type === "user_message" && e.deliveryStatus === "sending");
  }, [turns]);

  const handleScroll = useCallback(() => {
    const el = scrollRef.current;
    if (!el) return;
    setFollowing(isNearBottom(el));
  }, []);

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
    <div className="relative flex-1 min-h-0">
      <div
        ref={scrollRef}
        onScroll={handleScroll}
        className="h-full overflow-y-auto overflow-x-hidden [scrollbar-gutter:stable]"
      >
        <div className="p-4 max-md:px-2 space-y-8 min-w-0">
          {turns.map((turn, i) => {
            const eager = i >= turns.length - EAGER_TURN_COUNT;
            // If this turn has a compact_boundary, find the post-compaction
            // token count from the next turn's result event.
            const hasCompact = turn.events.some((e) => e.type === "compact_boundary");
            let postCompactTokens: number | undefined;
            if (hasCompact) {
              const nextResult = turns[i + 1]?.events.find(
                (e) => e.type === "result" && e.contextWindow && e.contextWindow > 0,
              );
              if (nextResult) {
                postCompactTokens = (nextResult.inputTokens ?? 0) + (nextResult.outputTokens ?? 0);
              }
            }
            const block = (
              <TurnBlock
                key={turn.id}
                turn={turn}
                isLast={i === turns.length - 1}
                sessionId={sessionId}
                projectId={projectId}
                sessionState={sessionState}
                projectPath={projectPath}
                worktreePath={worktreePath}
                showEvents={showEvents}
                postCompactTokens={postCompactTokens}
              />
            );
            if (eager) return block;
            return (
              <LazyTurn key={turn.id} scrollRoot={scrollRef}>
                {block}
              </LazyTurn>
            );
          })}
          {pendingMessages.map((msg) => (
            <UserMessage
              key={msg.messageId}
              prompt={msg.content ?? ""}
              attachments={msg.attachments}
              deliveryStatus="sending"
            />
          ))}
          <ScrollAnchor sessionId={sessionId} turns={turns} following={following} />
          <div ref={bottomRef} />
        </div>
      </div>
      <div className="absolute bottom-3 right-3 z-10 flex flex-col gap-1.5 pointer-events-none">
        <Tooltip>
          <TooltipTrigger asChild>
            <Button
              variant="secondary"
              size="icon"
              onClick={() => setShowEvents((v) => !v)}
              className="rounded-full shadow-lg opacity-60 hover:opacity-100 transition-opacity h-7 w-7 pointer-events-auto"
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
                className="rounded-full shadow-lg opacity-60 hover:opacity-100 transition-opacity h-7 w-7 pointer-events-auto"
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
