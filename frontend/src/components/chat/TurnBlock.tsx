import {
  AlertTriangle,
  Bot,
  Brain,
  Check,
  ChevronDown,
  ChevronRight,
  Copy,
  FileText,
  Loader2,
  Terminal,
  User,
} from "lucide-react";
import { useCallback, useEffect, useState } from "react";
import { createPortal } from "react-dom";
import { Markdown } from "~/components/chat/Markdown";
import { ThinkingBlock } from "~/components/chat/ThinkingBlock";
import { ToolResultBlock } from "~/components/chat/ToolResultBlock";
import { ToolUseBlock, formatSummary, getToolIcon } from "~/components/chat/ToolUseBlock";
import { Avatar, AvatarFallback } from "~/components/ui/avatar";
import { copyToClipboard } from "~/lib/utils";
import type { ChatEvent, Turn } from "~/stores/chat-store";
import { useStreamingStore } from "~/stores/streaming-store";

// --- Segment types ---

interface ThinkingSegment {
  kind: "thinking";
  events: ChatEvent[];
}
interface ToolSegment {
  kind: "tool";
  pairs: { use: ChatEvent; result?: ChatEvent }[];
}
interface TextSegment {
  kind: "text";
  content: string;
}
interface ErrorSegment {
  kind: "error";
  events: ChatEvent[];
}

type Segment = ThinkingSegment | ToolSegment | TextSegment | ErrorSegment;
type SegmentKind = Segment["kind"];

function classifyEvent(e: ChatEvent): SegmentKind | "result" | "skip" {
  switch (e.type) {
    case "thinking":
      return "thinking";
    case "tool_use":
    case "tool_result":
      return "tool";
    case "text":
      return "text";
    case "error":
      return "error";
    case "result":
      return "result";
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
        case "thinking":
          last.events.push(event);
          break;
        case "tool":
          if (event.type === "tool_use") {
            last.pairs.push({ use: event });
          } else {
            const pair = last.pairs.find((p) => p.use.toolId === event.toolId);
            if (pair) pair.result = event;
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
        case "thinking":
          segments.push({ kind: "thinking", events: [event] });
          break;
        case "tool":
          if (event.type === "tool_use") {
            segments.push({ kind: "tool", pairs: [{ use: event }] });
          } else {
            segments.push({ kind: "tool", pairs: [{ use: event }] });
          }
          break;
        case "text":
          segments.push({ kind: "text", content: event.content ?? "" });
          break;
        case "error":
          segments.push({ kind: "error", events: [event] });
          break;
      }
    }
  }

  return { segments, resultEvent };
}

function segmentKey(seg: Segment, i: number): string {
  switch (seg.kind) {
    case "thinking":
    case "error":
      return seg.events[0]?.id ?? `seg-${i}`;
    case "tool":
      return seg.pairs[0]?.use.id ?? `seg-${i}`;
    case "text":
      return `text-${i}`;
  }
}

// --- Shared components ---

interface TurnBlockProps {
  turn: Turn;
  isLast: boolean;
  sessionId: string;
  currentAssistantText: string;
  sessionState: string;
  projectPath?: string;
  worktreePath?: string;
  showEvents?: boolean;
}

function CollapsibleGroup({
  label,
  icon,
  count,
  defaultExpanded,
  statusContent,
  children,
}: {
  label: string;
  icon: React.ReactNode;
  count: number;
  defaultExpanded: boolean;
  statusContent?: React.ReactNode;
  children: React.ReactNode;
}) {
  const [expanded, setExpanded] = useState(defaultExpanded);
  return (
    <div className="border rounded-md bg-muted/30 overflow-hidden">
      <button
        type="button"
        onClick={() => setExpanded(!expanded)}
        className="flex items-center gap-2 px-2 py-1.5 text-xs text-muted-foreground w-full text-left hover:bg-muted/50 cursor-pointer transition-colors"
      >
        {expanded ? (
          <ChevronDown className="h-3 w-3 shrink-0" />
        ) : (
          <ChevronRight className="h-3 w-3 shrink-0" />
        )}
        {icon}
        <span>
          {count} {label}
        </span>
      </button>
      {!expanded && statusContent}
      {expanded && <div className="space-y-1 p-1 pt-0">{children}</div>}
    </div>
  );
}

function InFlightToolStatus({
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
    <div className="flex items-center gap-2 px-2 pb-1.5 text-xs text-muted-foreground min-w-0">
      <span className="w-3 shrink-0" />
      <Loader2 className="h-3 w-3 animate-spin shrink-0" />
      {getToolIcon(event.toolName ?? "Unknown", event.category)}
      <span className="font-medium shrink-0">{event.toolName}</span>
      {hasInput ? (
        <span className="text-muted-foreground/70 truncate min-w-0">{summary}</span>
      ) : streamingInput ? (
        <span className="text-muted-foreground/50 font-mono truncate min-w-0">
          {streamingInput}
        </span>
      ) : null}
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

function ThinkingSegmentView({ segment }: { segment: ThinkingSegment }) {
  if (segment.events.length === 1) {
    return <ThinkingBlock content={segment.events[0]?.content ?? ""} />;
  }
  return (
    <CollapsibleGroup
      label="thinking blocks"
      icon={<Brain className="h-3 w-3" />}
      count={segment.events.length}
      defaultExpanded={false}
    >
      {segment.events.map((e) => (
        <ThinkingBlock key={e.id} content={e.content ?? ""} />
      ))}
    </CollapsibleGroup>
  );
}

function ToolSegmentView({
  segment,
  isStreaming,
  sessionId,
  projectPath,
  worktreePath,
}: {
  segment: ToolSegment;
  isStreaming: boolean;
  sessionId: string;
  projectPath?: string;
  worktreePath?: string;
}) {
  const inFlightTool = isStreaming
    ? [...segment.pairs].reverse().find((p) => !p.result)?.use
    : undefined;

  return (
    <CollapsibleGroup
      label={segment.pairs.length === 1 ? "tool call" : "tool calls"}
      icon={<Terminal className="h-3 w-3" />}
      count={segment.pairs.length}
      defaultExpanded={false}
      statusContent={
        inFlightTool ? (
          <InFlightToolStatus
            event={inFlightTool}
            sessionId={sessionId}
            projectPath={projectPath}
            worktreePath={worktreePath}
          />
        ) : undefined
      }
    >
      {segment.pairs.map((pair) => (
        <div key={pair.use.id} className="space-y-1">
          <ToolUseBlock
            name={pair.use.toolName ?? "Unknown"}
            input={pair.use.toolInput}
            category={pair.use.category}
            toolId={pair.use.toolId}
            sessionId={sessionId}
            projectPath={projectPath}
            worktreePath={worktreePath}
          />
          {pair.result && <ToolResultBlock content={pair.result.content ?? ""} />}
        </div>
      ))}
    </CollapsibleGroup>
  );
}

function TextSegmentView({
  content,
  onCopy,
  copied,
}: {
  content: string;
  onCopy: (text: string) => void;
  copied: boolean;
}) {
  return (
    <div className="relative group/msg rounded-lg px-4 py-2 bg-muted">
      <button
        type="button"
        onClick={() => onCopy(content)}
        className="absolute top-2 right-2 p-1 rounded opacity-0 group-hover/msg:opacity-100 hover:bg-background/50 text-muted-foreground transition-opacity"
        aria-label="Copy message"
      >
        {copied ? <Check className="h-3.5 w-3.5" /> : <Copy className="h-3.5 w-3.5" />}
      </button>
      <Markdown content={content} />
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

export function TurnBlock({
  turn,
  isLast,
  sessionId,
  currentAssistantText,
  sessionState,
  projectPath,
  worktreePath,
  showEvents = true,
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

  const { segments, resultEvent } = buildSegments(turn.events);

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
    : segments.filter((s) => s.kind !== "thinking" && s.kind !== "tool").length;
  const hasAssistantContent = visibleSegmentCount > 0 || streamingTail || isStreaming;

  return (
    <div className="space-y-3">
      {/* User message */}
      <div className="flex gap-3 flex-row-reverse">
        <Avatar className="h-8 w-8 shrink-0">
          <AvatarFallback className="bg-primary text-primary-foreground">
            <User className="h-4 w-4" />
          </AvatarFallback>
        </Avatar>
        <div className="max-w-[75%] rounded-lg px-4 py-2 bg-primary text-primary-foreground">
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
          {turn.prompt && <p className="text-sm whitespace-pre-wrap">{turn.prompt}</p>}
        </div>
      </div>

      {/* Assistant response */}
      {hasAssistantContent && (
        <div className="flex gap-3">
          <Avatar className="h-8 w-8 shrink-0">
            <AvatarFallback className="bg-muted">
              <Bot className="h-4 w-4" />
            </AvatarFallback>
          </Avatar>
          <div className="flex-1 space-y-2 max-w-[85%] min-w-0 overflow-hidden">
            {/* Chronological segments */}
            {segments.map((seg, i) => {
              if (!showEvents && (seg.kind === "thinking" || seg.kind === "tool")) {
                // In compact mode, show in-flight tool status for the last tool segment while streaming
                if (seg.kind === "tool" && isStreaming && i === segments.length - 1) {
                  const inFlightTool = [...seg.pairs].reverse().find((p) => !p.result)?.use;
                  if (inFlightTool) {
                    return (
                      <InFlightToolStatus
                        key={segmentKey(seg, i)}
                        event={inFlightTool}
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
                case "thinking":
                  return <ThinkingSegmentView key={segmentKey(seg, i)} segment={seg} />;
                case "tool":
                  return (
                    <ToolSegmentView
                      key={segmentKey(seg, i)}
                      segment={seg}
                      isStreaming={isStreaming && i === segments.length - 1}
                      sessionId={sessionId}
                      projectPath={projectPath}
                      worktreePath={worktreePath}
                    />
                  );
                case "text":
                  return (
                    <TextSegmentView
                      key={segmentKey(seg, i)}
                      content={seg.content}
                      onCopy={handleCopy}
                      copied={copied}
                    />
                  );
                case "error":
                  return <ErrorSegmentView key={segmentKey(seg, i)} segment={seg} />;
              }
            })}

            {/* Streaming text tail (not yet committed as an event) */}
            {streamingTail && (
              <TextSegmentView content={streamingTail} onCopy={handleCopy} copied={copied} />
            )}

            {/* Streaming indicator — empty turn */}
            {isStreaming && segments.length === 0 && !streamingTail && (
              <div className="flex items-center gap-2 text-muted-foreground text-sm px-1">
                <Loader2 className="h-3.5 w-3.5 animate-spin" />
                <span>{sessionState === "running" ? "Working..." : "Connecting..."}</span>
              </div>
            )}

            {/* Streaming indicator — has content, still working */}
            {isStreaming && (segments.length > 0 || streamingTail) && (
              <div className="flex items-center gap-2 text-muted-foreground/60 text-xs px-1">
                <Loader2 className="h-3 w-3 animate-spin" />
              </div>
            )}

            {/* Result metadata */}
            {resultEvent && resultEvent.duration != null && resultEvent.duration > 0 && (
              <div className="text-xs text-muted-foreground">
                {(resultEvent.duration / 1000).toFixed(1)}s
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
}
