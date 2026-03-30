import { Bot, Check, Copy, Loader2, Scissors, Wrench } from "lucide-react";
import { memo, useMemo, useState } from "react";
import { createPortal } from "react-dom";
import { AgentMessage } from "~/components/chat/AgentMessage";
import { formatTokens } from "~/components/chat/ContextBar";
import { ErrorBlock } from "~/components/chat/ErrorBlock";
import { ExpandableRow } from "~/components/chat/ExpandableRow";
import { Markdown } from "~/components/chat/Markdown";
import { PromptGroupProvider } from "~/components/chat/PromptCard";
import { SubagentActivity } from "~/components/chat/SubagentActivity";
import { ThinkingBlock } from "~/components/chat/ThinkingBlock";
import { ThinkingIcon, ToolIcon } from "~/components/chat/ToolIcons";
import { ToolResultBlock } from "~/components/chat/ToolResultBlock";
import { ToolUseBlock, formatSummary } from "~/components/chat/ToolUseBlock";
import { UserMessage } from "~/components/chat/UserMessage";
import { Avatar, AvatarFallback } from "~/components/ui/avatar";
import { useCopyToClipboard } from "~/hooks/useCopyToClipboard";
import type { Attachment, ChatEvent, Turn } from "~/stores/chat-store";
import { useStreamingStore } from "~/stores/streaming-store";

const EMBEDDED_RESULT_TOOLS = new Set(["Bash", "Edit", "Write", "Glob", "TodoWrite"]);

// --- Segment types ---

type ActivityItem =
  | { kind: "thinking"; event: ChatEvent }
  | { kind: "tool"; use: ChatEvent; result?: ChatEvent; taskEvents?: ChatEvent[] };

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
interface UserMessageSegment {
  kind: "user_message";
  content: string;
  attachments?: Attachment[];
  deliveryStatus?: "sending" | "delivered";
}
interface AgentMessageSegment {
  kind: "agent_message";
  direction: "sent" | "received";
  content: string;
  senderName: string;
  senderSessionId: string;
  targetName: string;
  targetSessionId: string;
}

type Segment =
  | ActivitySegment
  | TextSegment
  | ErrorSegment
  | CompactSegment
  | UserMessageSegment
  | AgentMessageSegment;
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
    case "user_message":
      return "user_message";
    case "agent_message":
      return "agent_message";
    case "task":
      return "skip";
    default:
      return "skip";
  }
}

function buildSegments(events: ChatEvent[]): { segments: Segment[]; resultEvent?: ChatEvent } {
  const segments: Segment[] = [];
  let resultEvent: ChatEvent | undefined;

  // First pass: collect task events indexed by parent toolUseId
  const taskEventsByToolUseId = new Map<string, ChatEvent[]>();
  for (const event of events) {
    if (event.type === "task" && event.toolUseId) {
      let list = taskEventsByToolUseId.get(event.toolUseId);
      if (!list) {
        list = [];
        taskEventsByToolUseId.set(event.toolUseId, list);
      }
      list.push(event);
    }
  }

  for (const event of events) {
    const kind = classifyEvent(event);
    if (kind === "result") {
      resultEvent = event;
      continue;
    }
    if (kind === "skip") continue;

    const last = segments[segments.length - 1];

    // tool_result: find matching tool_use in any segment (may cross segment boundaries).
    if (event.type === "tool_result" && event.toolId) {
      for (let s = segments.length - 1; s >= 0; s--) {
        const seg = segments[s];
        if (seg?.kind !== "activity") continue;
        const item = seg.items.find((it) => it.kind === "tool" && it.use.toolId === event.toolId);
        if (item?.kind === "tool") {
          item.result = event;
          break;
        }
      }
      continue;
    }

    if (last?.kind === kind) {
      switch (last.kind) {
        case "activity":
          if (event.type === "thinking") {
            last.items.push({ kind: "thinking", event });
          } else if (event.type === "tool_use") {
            last.items.push({
              kind: "tool",
              use: event,
              taskEvents: event.toolId ? taskEventsByToolUseId.get(event.toolId) : undefined,
            });
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
            segments.push({
              kind: "activity",
              items: [
                {
                  kind: "tool",
                  use: event,
                  taskEvents: event.toolId ? taskEventsByToolUseId.get(event.toolId) : undefined,
                },
              ],
            });
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
        case "user_message":
          segments.push({
            kind: "user_message",
            content: event.content ?? "",
            attachments: event.attachments,
            deliveryStatus: event.deliveryStatus,
          });
          break;
        case "agent_message":
          segments.push({
            kind: "agent_message",
            direction: event.direction ?? "received",
            content: event.content ?? "",
            senderName: event.senderName ?? "",
            senderSessionId: event.senderSessionId ?? "",
            targetName: event.targetName ?? "",
            targetSessionId: event.targetSessionId ?? "",
          });
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
    case "user_message":
      return `user-msg-${i}`;
    case "agent_message":
      return `agent-msg-${i}`;
  }
}

// --- Shared components ---

interface TurnBlockProps {
  turn: Turn;
  isLast: boolean;
  sessionId: string;
  projectId: string;
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
          !expanded && trailingIcons ? (
            <span className="flex items-center gap-1.5 text-primary/40">{trailingIcons}</span>
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
}: {
  segment: ActivitySegment;
  isStreaming: boolean;
  sessionId: string;
  projectPath?: string;
  worktreePath?: string;
}) {
  const [lightboxSrc, setLightboxSrc] = useState<string | null>(null);
  const toolItems = segment.items.filter(
    (i): i is ActivityItem & { kind: "tool" } => i.kind === "tool",
  );
  const inFlightTool = isStreaming ? [...toolItems].reverse().find((i) => !i.result) : undefined;

  const trailingIcons = segment.items.map((item) => {
    if (item.kind === "thinking") {
      return (
        <span key={item.event.id} className="shrink-0">
          <ThinkingIcon />
        </span>
      );
    }
    return (
      <span key={item.use.id} className="shrink-0">
        <ToolIcon name={item.use.toolName ?? "Unknown"} category={item.use.category} />
      </span>
    );
  });

  return (
    <>
      <CollapsibleGroup
        title={activityTitle(segment.items)}
        icon={<Wrench className="h-3 w-3" />}
        defaultExpanded={false}
        trailingIcons={
          <span className="flex flex-row-reverse items-center gap-1.5 overflow-hidden">
            {[...trailingIcons].reverse()}
          </span>
        }
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
                resultContent={item.result?.contentBlocks}
              />
              {item.taskEvents && item.taskEvents.length > 0 && (
                <SubagentActivity taskEvents={item.taskEvents} />
              )}
              {item.result &&
                !EMBEDDED_RESULT_TOOLS.has(item.use.toolName ?? "") &&
                (item.result.contentBlocks ?? []).length > 0 && (
                  <ToolResultBlock
                    content={item.result.contentBlocks ?? []}
                    onImageClick={setLightboxSrc}
                  />
                )}
            </div>
          ),
        )}
      </CollapsibleGroup>

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
    </>
  );
});

const TextSegmentView = memo(function TextSegmentView({
  content,
  onCopy,
  copied,
  projectId,
  sessionId,
  isStreaming,
}: {
  content: string;
  onCopy: (text: string) => void;
  copied: boolean;
  projectId: string;
  sessionId: string;
  isStreaming: boolean;
}) {
  return (
    <PromptGroupProvider
      content={content}
      projectId={projectId}
      sessionId={sessionId}
      isStreaming={isStreaming}
    >
      <div className="group/msg rounded-lg px-4 py-2 bg-gradient-to-br from-agent/12 to-agent/6 shadow-lg shadow-black/30 border border-agent/10">
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
      <div className="flex-1 border-t border-dashed border-primary/30" />
      <span className="inline-flex items-center gap-1.5 rounded-full bg-primary/10 px-2.5 py-0.5 text-primary">
        <Scissors className="size-3" />
        {label} from {formatTokens(preTokens)}
        {postTokens != null ? ` to ${formatTokens(postTokens)}` : ""} tokens
      </span>
      <div className="flex-1 border-t border-dashed border-primary/30" />
    </div>
  );
}

function ErrorSegmentView({ segment }: { segment: ErrorSegment }) {
  return (
    <>
      {segment.events.map((e) => (
        <ErrorBlock key={e.id} event={e} />
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
  sessionState,
  projectPath,
  worktreePath,
  showEvents = true,
  postCompactTokens,
}: TurnBlockProps) {
  const { copied, copy: handleCopy } = useCopyToClipboard();
  const isStreaming = isLast && !turn.complete;

  // Subscribe to streaming text only when this is the active (last, incomplete) turn
  const currentAssistantText = useStreamingStore((s) =>
    isStreaming ? (s.texts[sessionId] ?? "") : "",
  );

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
      <UserMessage prompt={turn.prompt} attachments={turn.attachments} />

      {/* Assistant response */}
      {hasAssistantContent && (
        <div className="flex gap-3 max-md:flex-col max-md:gap-1">
          <Avatar className="h-8 w-8 shrink-0 max-md:h-6 max-md:w-6">
            <AvatarFallback className="bg-agent/15 text-agent">
              <Bot className="h-4 w-4 max-md:h-3 max-md:w-3" />
            </AvatarFallback>
          </Avatar>
          <div className="flex-1 space-y-3 max-w-[85%] max-md:max-w-full min-w-0 overflow-x-clip pr-2 max-md:pr-0">
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
                      sessionId={sessionId}
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
                case "user_message":
                  return (
                    <UserMessage
                      key={segmentKey(seg, i)}
                      prompt={seg.content}
                      attachments={seg.attachments}
                      deliveryStatus={seg.deliveryStatus}
                    />
                  );
                case "agent_message":
                  return (
                    <AgentMessage
                      key={segmentKey(seg, i)}
                      direction={seg.direction}
                      senderName={seg.senderName}
                      senderSessionId={seg.senderSessionId}
                      targetName={seg.targetName}
                      targetSessionId={seg.targetSessionId}
                      content={seg.content}
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
                sessionId={sessionId}
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
            {resultEvent && resultEvent.duration != null && resultEvent.duration > 0 && (
              <div className="text-xs text-muted-foreground flex items-center gap-1.5">
                <span>{(resultEvent.duration / 1000).toFixed(1)}s</span>
              </div>
            )}
          </div>
        </div>
      )}
    </div>
  );
});
