import { ArrowDown, Loader2 } from "lucide-react";
import {
  memo,
  type ReactNode,
  useCallback,
  useEffect,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
} from "react";
import { TurnBlock } from "~/components/chat/TurnBlock";
import { UserMessage } from "~/components/chat/UserMessage";
import { Button } from "~/components/ui/button";
import { Tooltip, TooltipContent, TooltipTrigger } from "~/components/ui/tooltip";
import { ANIMATE_CHAT, useAutoAnimate } from "~/hooks/useAutoAnimate";
import type { ChatEvent, ResultEvent, Turn, UserMessageEvent } from "~/stores/chat-store";
import { useChatStore } from "~/stores/chat-store";
import { useStreamingStore } from "~/stores/streaming-store";

const SCROLL_THRESHOLD = 48;
const EAGER_TURN_COUNT = 4;
const EMPTY_PENDING: ChatEvent[] = [];
const EMPTY_USER_MESSAGES: UserMessageEvent[] = [];

/** Per-session scroll position, preserved across tab switches and remounts. */
const scrollMemory = new Map<string, { scrollTop: number; atBottom: boolean }>();

function isNearBottom(el: HTMLElement): boolean {
  return el.scrollHeight - el.scrollTop - el.clientHeight < SCROLL_THRESHOLD;
}

/** Style that lets the browser skip layout/paint for off-screen turns entirely. */
const CONTENT_VISIBILITY_STYLE: React.CSSProperties = {
  contentVisibility: "auto",
  containIntrinsicSize: "auto 200px",
};

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

  // Placeholder height matches containIntrinsicSize so scrollHeight estimates
  // correctly before turns mount — prevents large cumulative layout shift as
  // IntersectionObserver fires turn-by-turn.
  if (!visible) return <div ref={ref} className="min-h-[200px]" style={CONTENT_VISIBILITY_STYLE} />;
  return <div style={CONTENT_VISIBILITY_STYLE}>{children}</div>;
}

const SCROLL_POLL_MS = 100;
/** Max px distance for smooth scroll — beyond this, snap instantly. */
const SMOOTH_SCROLL_MAX_PX = 1200;

/** Manages auto-scroll: interval-based during streaming, event-driven otherwise. */
const ScrollAnchor = memo(function ScrollAnchor({
  sessionId,
  turns,
  scrollContainer,
  following,
}: {
  sessionId: string;
  turns: Turn[];
  scrollContainer: React.RefObject<HTMLDivElement | null>;
  following: boolean;
}) {
  const bottomRef = useRef<HTMLDivElement>(null);
  const isStreaming = useStreamingStore((s) => sessionId in s.texts);
  const prevStreamingRef = useRef(isStreaming);
  const prevTurnCountRef = useRef(turns.length);
  const prevSessionIdRef = useRef(sessionId);

  // During streaming: poll at 10fps for smooth auto-scroll without per-delta re-renders.
  useEffect(() => {
    if (!following || !isStreaming) return;
    const id = setInterval(() => {
      bottomRef.current?.scrollIntoView({ behavior: "instant" });
    }, SCROLL_POLL_MS);
    return () => clearInterval(id);
  }, [following, isStreaming]);

  // Non-streaming: scroll on turn count changes (new turn, completion metadata).
  useEffect(() => {
    const sessionChanged = prevSessionIdRef.current !== sessionId;
    if (sessionChanged) {
      prevSessionIdRef.current = sessionId;
      prevStreamingRef.current = false;
      prevTurnCountRef.current = turns.length;
      // Always snap to bottom on session switch.
      requestAnimationFrame(() => {
        bottomRef.current?.scrollIntoView({ behavior: "instant" });
      });
      return;
    }
    const prevCount = prevTurnCountRef.current;
    prevTurnCountRef.current = turns.length;
    if (!following || isStreaming) return;

    // History backfill prepends older turns — turn count grows but the new
    // content is above the viewport. Skip scroll to avoid jumping.
    const grewByMany = turns.length - prevCount > 3;
    if (grewByMany) return;

    const wasStreaming = prevStreamingRef.current;
    prevStreamingRef.current = isStreaming;
    const newTurn = turns.length > prevCount;

    // Snap instantly for large distances or new turns; smooth for small adjustments.
    const el = scrollContainer.current;
    const distToBottom = el ? el.scrollHeight - el.scrollTop - el.clientHeight : 0;
    const farAway = distToBottom > SMOOTH_SCROLL_MAX_PX;
    const behavior: ScrollBehavior = wasStreaming || newTurn || farAway ? "instant" : "smooth";

    requestAnimationFrame(() => {
      bottomRef.current?.scrollIntoView({ behavior });
    });
  }, [turns, following, isStreaming, sessionId, scrollContainer]);

  return <div ref={bottomRef} />;
});

function HistoryBackfillIndicator() {
  return (
    <div className="space-y-6 animate-pulse">
      {/* Ghost turn 1: short prompt + medium response */}
      <div className="space-y-2">
        <div className="h-4 w-48 rounded bg-muted/60" />
        <div className="space-y-1.5 pl-1">
          <div className="h-3 w-full rounded bg-muted/40" />
          <div className="h-3 w-3/4 rounded bg-muted/40" />
        </div>
      </div>
      {/* Ghost turn 2: medium prompt + long response */}
      <div className="space-y-2">
        <div className="h-4 w-64 rounded bg-muted/60" />
        <div className="space-y-1.5 pl-1">
          <div className="h-3 w-full rounded bg-muted/40" />
          <div className="h-3 w-full rounded bg-muted/40" />
          <div className="h-3 w-1/2 rounded bg-muted/40" />
        </div>
      </div>
      {/* Ghost turn 3: short */}
      <div className="space-y-2">
        <div className="h-4 w-36 rounded bg-muted/60" />
        <div className="space-y-1.5 pl-1">
          <div className="h-3 w-5/6 rounded bg-muted/40" />
        </div>
      </div>
      <div className="flex items-center gap-2 text-xs text-muted-foreground">
        <Loader2 className="h-3 w-3 animate-spin" />
        <span>Loading earlier messages</span>
      </div>
    </div>
  );
}

interface MessageListProps {
  turns: Turn[];
  sessionId: string;
  projectId: string;
  sessionState: string;
  projectPath?: string;
  worktreePath?: string;
  isLoadingHistory?: boolean;
  /** True when we have a tail cache and are loading the full history in the background. */
  isBackfilling?: boolean;
}

export function MessageList({
  turns,
  sessionId,
  projectId,
  sessionState,
  projectPath,
  worktreePath,
  isLoadingHistory,
  isBackfilling,
}: MessageListProps) {
  const scrollRef = useRef<HTMLDivElement>(null);
  const contentRef = useRef<HTMLDivElement>(null);
  const bottomRef = useRef<HTMLDivElement>(null);
  const pendingRestoreRef = useRef<number | null>(null);
  const [animateRef, setAnimateEnabled] = useAutoAnimate<HTMLDivElement>(ANIMATE_CHAT);
  // animateRef is stable but calls setState internally; an inline ref callback
  // would change identity each render, causing React to re-invoke it and loop
  // on setState (max update depth, error #185).
  const setContentRef = useCallback(
    (node: HTMLDivElement | null) => {
      animateRef(node);
      contentRef.current = node;
    },
    [animateRef],
  );
  const savedInitial = scrollMemory.get(sessionId);
  const initialFollowing = !savedInitial || savedInitial.atBottom;
  const [following, setFollowing] = useState(initialFollowing);
  const followingRef = useRef(following);
  followingRef.current = following;
  const prevSessionRef = useRef(sessionId);
  if (prevSessionRef.current !== sessionId) {
    const prevId = prevSessionRef.current;
    prevSessionRef.current = sessionId;
    // Flush outgoing session's live scroll state in case a throttled rAF was
    // still pending when the user swapped tabs.
    const el = scrollRef.current;
    if (el) {
      scrollMemory.set(prevId, { scrollTop: el.scrollTop, atBottom: isNearBottom(el) });
    }
    const saved = scrollMemory.get(sessionId);
    const restoreToBottom = !saved || saved.atBottom;
    setFollowing(restoreToBottom);
    followingRef.current = restoreToBottom;
    pendingRestoreRef.current = restoreToBottom ? null : saved.scrollTop;
    // Disable auto-animate during session switch so turn DOM swaps are instant
    // (avoids the zoom/slide effect from FLIP removal animations).
    setAnimateEnabled(false);
  }

  // Restore saved scroll position after layout for session switches where the
  // user was mid-scroll. Runs every render; sentinel ref makes it a no-op
  // outside actual switches.
  useLayoutEffect(() => {
    const pending = pendingRestoreRef.current;
    if (pending == null) return;
    pendingRestoreRef.current = null;
    const el = scrollRef.current;
    if (!el) return;
    el.scrollTop = pending;
  });
  const isAnyStreaming = useStreamingStore((s) => sessionId in s.texts);
  const hasIncompleteTurn = turns.length > 0 && !turns[turns.length - 1]?.complete;

  // Disable auto-animate during active turns — MutationObserver + FLIP calculations
  // are pure overhead when DOM mutations are rapid, and removal animations (position:
  // absolute) cause pending messages to float over content during delivery transitions.
  useEffect(() => {
    setAnimateEnabled(!isAnyStreaming && !hasIncompleteTurn);
  }, [isAnyStreaming, hasIncompleteTurn, setAnimateEnabled]);

  // Re-pin bottom whenever content grows while the user is following. Handles
  // layout deferrals on session switch: lazy-mounted turns, content-visibility
  // size estimates resolving to real sizes, images/code highlighting settling
  // across several frames. Without this, a single scroll-to-bottom fires before
  // layout completes and the viewport ends up slightly above the last message.
  useEffect(() => {
    const scrollEl = scrollRef.current;
    const contentEl = contentRef.current;
    if (!scrollEl || !contentEl) return;
    const observer = new ResizeObserver(() => {
      if (!followingRef.current) return;
      scrollEl.scrollTop = scrollEl.scrollHeight;
    });
    observer.observe(contentEl);
    return () => observer.disconnect();
  }, []);

  // Pending user messages live in streamingEvents during streaming.
  const streamingEvents = useChatStore(
    (s) => s.sessions[sessionId]?.streamingEvents ?? EMPTY_PENDING,
  );
  const pendingMessages = useMemo(() => {
    if (streamingEvents.length === 0) return EMPTY_USER_MESSAGES;
    return streamingEvents.filter(
      (e): e is UserMessageEvent => e.type === "user_message" && e.deliveryStatus === "sending",
    );
  }, [streamingEvents]);

  const scrollRafRef = useRef<number | null>(null);
  const handleScroll = useCallback(() => {
    if (scrollRafRef.current != null) return;
    scrollRafRef.current = requestAnimationFrame(() => {
      scrollRafRef.current = null;
      const el = scrollRef.current;
      if (!el) return;
      const atBottom = isNearBottom(el);
      setFollowing(atBottom);
      scrollMemory.set(sessionId, { scrollTop: el.scrollTop, atBottom });
    });
  }, [sessionId]);

  useEffect(
    () => () => {
      if (scrollRafRef.current != null) cancelAnimationFrame(scrollRafRef.current);
    },
    [],
  );

  const scrollToBottom = useCallback(() => {
    const el = scrollRef.current;
    if (!el) return;
    const dist = el.scrollHeight - el.scrollTop - el.clientHeight;
    const behavior: ScrollBehavior = dist > SMOOTH_SCROLL_MAX_PX ? "instant" : "smooth";
    bottomRef.current?.scrollIntoView({ behavior });
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
        <div ref={setContentRef} className="py-4 pl-5 pr-4 max-md:px-2 space-y-8 min-w-0">
          {isBackfilling && <HistoryBackfillIndicator />}
          {turns.map((turn, i) => {
            const eager = i >= turns.length - EAGER_TURN_COUNT;
            // If this turn has a compact_boundary, find the post-compaction
            // token count from the next turn's result event.
            const hasCompact = turn.events.some((e) => e.type === "compact_boundary");
            let postCompactTokens: number | undefined;
            if (hasCompact) {
              const nextResult = turns[i + 1]?.events.find(
                (e): e is ResultEvent =>
                  e.type === "result" && !!e.contextWindow && e.contextWindow > 0,
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
          {pendingMessages.map((msg, i) => (
            <UserMessage
              key={msg.messageId ?? `pending-${i}`}
              prompt={msg.content ?? ""}
              attachments={msg.attachments}
              deliveryStatus="sending"
            />
          ))}
          <ScrollAnchor
            sessionId={sessionId}
            turns={turns}
            scrollContainer={scrollRef}
            following={following}
          />
          <div ref={bottomRef} />
        </div>
      </div>
      {!following && (
        <div className="absolute bottom-3 right-3 z-10 pointer-events-none">
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
        </div>
      )}
    </div>
  );
}
