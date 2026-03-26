import {
  AlertTriangle,
  Bot,
  Check,
  Copy,
  FileText,
  Loader2,
  Scissors,
  User,
  Wrench,
} from "lucide-react";
import { memo, useCallback, useEffect, useMemo, useState } from "react";
import { createPortal } from "react-dom";
import { formatTokens } from "~/components/chat/ContextBar";
import { ExpandableRow } from "~/components/chat/ExpandableRow";
import { Markdown } from "~/components/chat/Markdown";
import { PromptGroupProvider } from "~/components/chat/PromptCard";
import { ThinkingBlock } from "~/components/chat/ThinkingBlock";
import { ThinkingIcon, ToolIcon } from "~/components/chat/ToolIcons";
import { ToolResultBlock } from "~/components/chat/ToolResultBlock";
import { ToolUseBlock, formatSummary } from "~/components/chat/ToolUseBlock";
import { Avatar, AvatarFallback } from "~/components/ui/avatar";
import { copyToClipboard } from "~/lib/utils";
import type { ChatEvent, Turn } from "~/stores/chat-store";
import { useStreamingStore } from "~/stores/streaming-store";

// --- Segment types ---

type ActivityItem =
  | { kind: "thinking"; event: ChatEvent }
  | { kind: "tool"; use: ChatEvent; result?: ChatEvent };

interface ActivitySegment {
  kind: "activity";
  items: ActivityItem[];
}
interface TextSegment {
  kind: "text";
  content: string;
}
interface ErrorSegment {
  kind: "error";
  events: ChatEvent[];
}
interface CompactSegment {
  kind: "compact";
  event: ChatEvent;
}

type Segment = ActivitySegment | TextSegment | ErrorSegment | CompactSegment;
type SegmentKind = Segment["kind"];

function classifyEvent(e: ChatEvent): SegmentKind | "result" | "skip" {
  switch (e.type) {
    case "thinking":
    case "tool_use":
    case "tool_result":
      return "activity";
    case "text":
      return "text";
    case "error":
      return "error";
    case "result":
      return "result";
    case "compact_boundary":
      return "compact";
    default:
      return "skip";
  }
}

function buildSegments(events: ChatEvent[]): { segments: Segment[]; resultEvent?: ChatEvent } {
  const segments: Segment[] = [];
  let resultEvent: ChatEvent | undefined;

  for (const event of events) {
    const kind = classifyEvent(event);
    if (kind === "result") {
      resultEvent = event;
      continue;
    }
    if (kind === "skip") continue;

    const last = segments[segments.length - 1];

    if (last?.kind === kind) {
      switch (last.kind) {
        case "activity":
          if (event.type === "thinking") {
            last.items.push({ kind: "thinking", event });
          } else if (event.type === "tool_use") {
            last.items.push({ kind: "tool", use: event });
          } else {
            const item = last.items.find(
              (it) => it.kind === "tool" && it.use.toolId === event.toolId,
            );
            if (item?.kind === "tool") item.result = event;
          }
          break;
        case "text":
          last.content += `\n\n${event.content ?? ""}`;
          break;
        case "error":
          last.events.push(event);
          break;
      }
    } else {
      switch (kind) {
        case "activity":
          if (event.type === "thinking") {
            segments.push({ kind: "activity", items: [{ kind: "thinking", event }] });
          } else if (event.type === "tool_use") {
            segments.push({ kind: "activity", items: [{ kind: "tool", use: event }] });
          } else {
            segments.push({ kind: "activity", items: [{ kind: "tool", use: event }] });
          }
          break;
        case "text":
          segments.push({ kind: "text", content: event.content ?? "" });
          break;
        case "error":
          segments.push({ kind: "error", events: [event] });
          break;
        case "compact":
          segments.push({ kind: "compact", event });
          break;
      }
    }
  }

  return { segments, resultEvent };
}

function segmentKey(seg: Segment, i: number): string {
  switch (seg.kind) {
    case "activity": {
      const first = seg.items[0];
      if (!first) return `seg-${i}`;
      return (first.kind === "thinking" ? first.event.id : first.use.id) ?? `seg-${i}`;
    }
    case "error":
      return seg.events[0]?.id ?? `seg-${i}`;
    case "text":
      return `text-${i}`;
    case "compact":
      return seg.event.id ?? `compact-${i}`;
  }
}

// --- Shared components ---

interface TurnBlockProps {
  turn: Turn;
  isLast: boolean;
  sessionId: string;
  projectId: string;
  currentAssistantText: string;
  sessionState: string;
  projectPath?: string;
  worktreePath?: string;
  showEvents?: boolean;
  postCompactTokens?: number;
}

function CollapsibleGroup({
  title,
  icon,
  defaultExpanded,
  activeHeader,
  trailingIcons,
  children,
}: {
  title: string;
  icon: React.ReactNode;
  defaultExpanded: boolean;
  activeHeader?: React.ReactNode;
  trailingIcons?: React.ReactNode;
  children: React.ReactNode;
}) {
  const [expanded, setExpanded] = useState(defaultExpanded);
  const showActiveHeader = !!activeHeader && !expanded;

  return (
    <div className="border rounded-md bg-muted/30 overflow-hidden">
      <ExpandableRow
        expanded={expanded}
        onToggle={() => setExpanded(!expanded)}
        className="hover:bg-muted/50"
        trailing={
          trailingIcons && !showActiveHeader ? (
            <span className="flex items-center gap-1.5 text-[#7aa2f7]/40">{trailingIcons}</span>
          ) : undefined
        }
      >
        {showActiveHeader ? (
          activeHeader
        ) : (
          <>
            {icon}
            <span>{title}</span>
          </>
        )}
      </ExpandableRow>
      {expanded && <div className="space-y-2 p-1.5 pt-1">{children}</div>}
    </div>
  );
}

function InFlightToolContent({
  event,
  sessionId,
  projectPath,
  worktreePath,
}: {
  event: ChatEvent;
  sessionId: string;
  projectPath?: string;
  worktreePath?: string;
}) {
  const streamingInput = useStreamingStore((s) =>
    event.toolId ? s.toolInputs[sessionId]?.[event.toolId] : undefined,
  );
  const hasInput = !!event.toolInput;
  const summary = hasInput
    ? formatSummary(event.toolName ?? "", event.toolInput, projectPath, worktreePath)
    : "";

  return (
    <>
      <Loader2 className="h-3 w-3 animate-spin shrink-0" />
      <span className="font-medium shrink-0">{event.toolName}</span>
      {hasInput ? (
        <span className="text-muted-foreground/70 truncate min-w-0">{summary}</span>
      ) : streamingInput ? (
        <span className="text-muted-foreground/50 font-mono truncate min-w-0">
          {streamingInput}
        </span>
      ) : null}
    </>
  );
}

function InFlightToolStatus(props: {
  event: ChatEvent;
  sessionId: string;
  projectPath?: string;
  worktreePath?: string;
}) {
  return (
    <div className="flex items-center gap-2 px-2 pb-1.5 text-xs text-muted-foreground min-w-0">
      <span className="w-3 shrink-0" />
      <InFlightToolContent {...props} />
    </div>
  );
}

function formatErrorMessage(event: ChatEvent): string {
  if (event.errorType === "rate_limit") {
    return event.retryAfterSecs
      ? `Rate limited — retry in ${event.retryAfterSecs}s`
      : "Rate limited";
  }
  if (event.errorType === "auth") return "Authentication error";
  if (event.errorType === "overloaded") return "API overloaded — try again shortly";
  return event.content ?? "Unknown error";
}

// --- Segment renderers ---

function activityTitle(items: ActivityItem[]): string {
  const thoughts = items.filter((i) => i.kind === "thinking").length;
  const tools = items.filter((i) => i.kind === "tool").length;
  const parts: string[] = [];
  if (thoughts > 0) parts.push(`${thoughts} ${thoughts === 1 ? "thought" : "thoughts"}`);
  if (tools > 0) parts.push(`${tools} ${tools === 1 ? "tool call" : "tool calls"}`);
  return parts.join(" and ");
}

const ActivitySegmentView = memo(function ActivitySegmentView({
  segment,
  isStreaming,
  sessionId,
  projectPath,
  worktreePath,
  onImageClick,
}: {
  segment: ActivitySegment;
  isStreaming: boolean;
  sessionId: string;
  projectPath?: string;
  worktreePath?: string;
  onImageClick?: (src: string) => void;
}) {
  const toolItems = segment.items.filter(
    (i): i is ActivityItem & { kind: "tool" } => i.kind === "tool",
  );
  const inFlightTool = isStreaming ? [...toolItems].reverse().find((i) => !i.result) : undefined;

  const trailingIcons = segment.items.slice(0, 12).map((item) => {
    if (item.kind === "thinking") {
      return (
        <span key={item.event.id}>
          <ThinkingIcon />
        </span>
      );
    }
    return (
      <span key={item.use.id}>
        <ToolIcon name={item.use.toolName ?? "Unknown"} category={item.use.category} />
      </span>
    );
  });

  return (
    <CollapsibleGroup
      title={activityTitle(segment.items)}
      icon={<Wrench className="h-3 w-3" />}
      defaultExpanded={false}
      trailingIcons={trailingIcons}
      activeHeader={
        inFlightTool ? (
          <InFlightToolContent
            event={inFlightTool.use}
            sessionId={sessionId}
            projectPath={projectPath}
            worktreePath={worktreePath}
          />
        ) : undefined
      }
    >
      {segment.items.map((item) =>
        item.kind === "thinking" ? (
          <ThinkingBlock key={item.event.id} content={item.event.content ?? ""} />
        ) : (
          <div key={item.use.id} className="space-y-1.5">
            <ToolUseBlock
              name={item.use.toolName ?? "Unknown"}
              input={item.use.toolInput}
              category={item.use.category}
              toolId={item.use.toolId}
              sessionId={sessionId}
              projectPath={projectPath}
              worktreePath={worktreePath}
            />
            {item.result && (
              <ToolResultBlock
                content={item.result.contentBlocks ?? []}
                onImageClick={onImageClick}
              />
            )}
          </div>
        ),
      )}
    </CollapsibleGroup>
  );
});

const TextSegmentView = memo(function TextSegmentView({
  content,
  onCopy,
  copied,
  projectId,
  isStreaming,
}: {
  content: string;
  onCopy: (text: string) => void;
  copied: boolean;
  projectId: string;
  isStreaming: boolean;
}) {
  return (
    <PromptGroupProvider content={content} projectId={projectId} isStreaming={isStreaming}>
      <div className="group/msg rounded-lg px-4 py-2 bg-gradient-to-br from-[#bb9af7]/12 to-[#bb9af7]/6 shadow-lg shadow-black/30 border border-[#bb9af7]/10">
        <button
          type="button"
          onClick={() => onCopy(content)}
          className="sticky top-2 float-right ml-2 p-1 rounded max-md:opacity-60 opacity-0 group-hover/msg:opacity-100 hover:bg-background/50 text-muted-foreground transition-opacity z-10"
          aria-label="Copy message"
        >
          {copied ? <Check className="h-3.5 w-3.5" /> : <Copy className="h-3.5 w-3.5" />}
        </button>
        <Markdown content={content} />
      </div>
    </PromptGroupProvider>
  );
});

function CompactDivider({ event, postTokens }: { event: ChatEvent; postTokens?: number }) {
  const preTokens = event.preTokens ?? 0;
  const label = event.trigger === "manual" ? "Manual compaction" : "Auto-compacted";
  return (
    <div className="flex items-center gap-2 text-xs text-muted-foreground/80 py-2 -mx-4">
      <div className="flex-1 border-t border-dashed border-blue-500/30" />
      <span className="inline-flex items-center gap-1.5 rounded-full bg-blue-500/10 px-2.5 py-0.5 text-blue-400">
        <Scissors className="size-3" />
        {label} from {formatTokens(preTokens)}
        {postTokens != null ? ` to ${formatTokens(postTokens)}` : ""} tokens
      </span>
      <div className="flex-1 border-t border-dashed border-blue-500/30" />
    </div>
  );
}

function ErrorSegmentView({ segment }: { segment: ErrorSegment }) {
  return (
    <>
      {segment.events.map((e) => (
        <div
          key={e.id}
          className={`rounded-lg px-4 py-2 text-sm flex items-center gap-2 ${
            e.errorType === "rate_limit" || e.errorType === "overloaded"
              ? "bg-yellow-500/10 text-yellow-700 dark:text-yellow-400"
              : "bg-destructive/10 text-destructive"
          }`}
        >
          {(e.errorType === "rate_limit" || e.errorType === "overloaded") && (
            <AlertTriangle className="h-4 w-4 shrink-0" />
          )}
          <span>{formatErrorMessage(e)}</span>
        </div>
      ))}
    </>
  );
}

// --- Main component ---

export const TurnBlock = memo(function TurnBlock({
  turn,
  isLast,
  sessionId,
  projectId,
  currentAssistantText,
  sessionState,
  projectPath,
  worktreePath,
  showEvents = true,
  postCompactTokens,
}: TurnBlockProps) {
  const [copied, setCopied] = useState(false);
  const [lightboxSrc, setLightboxSrc] = useState<string | null>(null);
  const isStreaming = isLast && !turn.complete;

  const handleCopy = useCallback((text: string) => {
    copyToClipboard(text).then(() => {
      setCopied(true);
      setTimeout(() => setCopied(false), 1500);
    });
  }, []);

  useEffect(() => {
    if (!lightboxSrc) return;
    const handleKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") setLightboxSrc(null);
    };
    document.addEventListener("keydown", handleKey);
    return () => document.removeEventListener("keydown", handleKey);
  }, [lightboxSrc]);

  const { segments, resultEvent } = useMemo(() => buildSegments(turn.events), [turn.events]);

  // Compute streaming tail: text being actively streamed that hasn't been committed as an event yet
  let streamingTail = "";
  if (isStreaming && currentAssistantText) {
    const committedText = turn.events
      .filter((e) => e.type === "text")
      .map((e) => e.content ?? "")
      .join("\n\n");
    if (committedText && currentAssistantText.startsWith(committedText)) {
      streamingTail = currentAssistantText.slice(committedText.length).replace(/^\n\n/, "");
    } else if (!committedText) {
      streamingTail = currentAssistantText;
    }
  }

  const visibleSegmentCount = showEvents
    ? segments.length
    : segments.filter((s) => s.kind !== "activity").length;
  const hasAssistantContent = visibleSegmentCount > 0 || streamingTail || isStreaming;

  return (
    <div className="space-y-4">
      {/* User message */}
      <div className="flex gap-3 flex-row-reverse">
        <Avatar className="h-8 w-8 shrink-0">
          <AvatarFallback className="bg-[#7aa2f7]/20 text-[#7aa2f7]">
            <User className="h-4 w-4" />
          </AvatarFallback>
        </Avatar>
        <div className="group/usermsg relative max-w-[75%] rounded-lg px-4 py-2 bg-gradient-to-br from-[#7aa2f7]/20 to-[#7aa2f7]/10 text-foreground shadow-lg shadow-black/30 border border-[#7aa2f7]/10">
          {turn.prompt && (
            <button
              type="button"
              onClick={() => turn.prompt && handleCopy(turn.prompt)}
              className="absolute -left-8 max-md:-top-6 max-md:left-auto max-md:right-0 top-1 p-1 rounded max-md:opacity-60 opacity-0 group-hover/usermsg:opacity-100 hover:bg-muted text-muted-foreground transition-opacity z-10"
              aria-label="Copy message"
            >
              {copied ? <Check className="h-3.5 w-3.5" /> : <Copy className="h-3.5 w-3.5" />}
            </button>
          )}
          {turn.attachments && turn.attachments.length > 0 && (
            <div className="flex gap-1.5 flex-wrap mb-2">
              {turn.attachments.map((a) =>
                a.mimeType.startsWith("image/") ? (
                  <button
                    key={a.id}
                    type="button"
                    className="p-0 border-none bg-transparent cursor-pointer"
                    onClick={() => setLightboxSrc(a.dataUrl)}
                  >
                    <img
                      src={a.previewUrl ?? a.dataUrl}
                      alt={a.name}
                      className="h-20 max-w-[200px] object-cover rounded"
                    />
                  </button>
                ) : (
                  <div
                    key={a.id}
                    className="h-20 w-20 rounded bg-primary-foreground/10 flex flex-col items-center justify-center gap-1 px-1"
                  >
                    <FileText className="h-5 w-5" />
                    <span className="text-[9px] truncate w-full text-center">{a.name}</span>
                  </div>
                ),
              )}
            </div>
          )}
          {turn.prompt && (
            <Markdown content={turn.prompt} className="prose-user" preserveNewlines />
          )}
        </div>
      </div>

      {/* Assistant response */}
      {hasAssistantContent && (
        <div className="flex gap-3">
          <Avatar className="h-8 w-8 shrink-0">
            <AvatarFallback className="bg-[#bb9af7]/15 text-[#bb9af7]">
              <Bot className="h-4 w-4" />
            </AvatarFallback>
          </Avatar>
          <div className="flex-1 space-y-3 max-w-[85%] min-w-0 overflow-x-clip pr-2">
            {/* Chronological segments */}
            {segments.map((seg, i) => {
              if (!showEvents && seg.kind === "activity") {
                if (isStreaming && i === segments.length - 1) {
                  const toolItems = seg.items.filter(
                    (it): it is ActivityItem & { kind: "tool" } => it.kind === "tool",
                  );
                  const inFlightTool = [...toolItems].reverse().find((t) => !t.result);
                  if (inFlightTool) {
                    return (
                      <InFlightToolStatus
                        key={segmentKey(seg, i)}
                        event={inFlightTool.use}
                        sessionId={sessionId}
                        projectPath={projectPath}
                        worktreePath={worktreePath}
                      />
                    );
                  }
                }
                return null;
              }
              switch (seg.kind) {
                case "activity":
                  return (
                    <ActivitySegmentView
                      key={segmentKey(seg, i)}
                      segment={seg}
                      isStreaming={isStreaming && i === segments.length - 1}
                      sessionId={sessionId}
                      projectPath={projectPath}
                      worktreePath={worktreePath}
                      onImageClick={setLightboxSrc}
                    />
                  );
                case "text":
                  return (
                    <TextSegmentView
                      key={segmentKey(seg, i)}
                      content={seg.content}
                      onCopy={handleCopy}
                      copied={copied}
                      projectId={projectId}
                      isStreaming={false}
                    />
                  );
                case "error":
                  return <ErrorSegmentView key={segmentKey(seg, i)} segment={seg} />;
                case "compact":
                  return (
                    <CompactDivider
                      key={segmentKey(seg, i)}
                      event={seg.event}
                      postTokens={postCompactTokens}
                    />
                  );
              }
            })}

            {/* Streaming text tail (not yet committed as an event) */}
            {streamingTail && (
              <TextSegmentView
                content={streamingTail}
                onCopy={handleCopy}
                copied={copied}
                projectId={projectId}
                isStreaming
              />
            )}

            {/* Streaming indicator — empty turn */}
            {isStreaming && segments.length === 0 && !streamingTail && (
              <div className="flex items-center gap-2 text-muted-foreground text-sm px-1">
                <Loader2 className="h-3.5 w-3.5 animate-spin" />
                <span>{sessionState === "running" ? "Working..." : "Connecting..."}</span>
              </div>
            )}

            {/* Streaming indicator — has content, still working (hidden when in-flight tool already shows a spinner) */}
            {isStreaming &&
              (segments.length > 0 || streamingTail) &&
              (() => {
                const last = segments[segments.length - 1];
                const hasInFlightTool =
                  last?.kind === "activity" &&
                  last.items.some((it) => it.kind === "tool" && !it.result);
                return !hasInFlightTool;
              })() && (
                <div className="flex items-center gap-2 text-muted-foreground/60 text-xs px-1">
                  <Loader2 className="h-3 w-3 animate-spin" />
                </div>
              )}

            {/* Result metadata */}
            {resultEvent && (resultEvent.duration || resultEvent.cost) && (
              <div className="text-xs text-muted-foreground flex items-center gap-1.5">
                {resultEvent.duration != null && resultEvent.duration > 0 && (
                  <span>{(resultEvent.duration / 1000).toFixed(1)}s</span>
                )}
                {resultEvent.cost != null && resultEvent.cost > 0 && (
                  <span className="tabular-nums">${resultEvent.cost.toFixed(2)}</span>
                )}
              </div>
            )}
          </div>
        </div>
      )}

      {lightboxSrc &&
        createPortal(
          <dialog
            open
            className="fixed inset-0 z-50 bg-black/80 flex items-center justify-center cursor-pointer m-0 p-0 border-none max-w-none max-h-none w-screen h-screen"
            onClick={() => setLightboxSrc(null)}
            onKeyDown={(e) => {
              if (e.key === "Escape") setLightboxSrc(null);
            }}
            aria-label="Image preview"
          >
            <img
              src={lightboxSrc}
              alt="Full-size preview"
              className="max-h-[90vh] max-w-[90vw] object-contain rounded-lg"
            />
          </dialog>,
          document.body,
        )}
    </div>
  );
});
