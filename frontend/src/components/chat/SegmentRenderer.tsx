import { Check, Copy, Loader2, Scissors, Wrench } from "lucide-react";
import { memo, useState } from "react";
import { AgentMessage } from "~/components/chat/AgentMessage";
import { formatTokens } from "~/components/chat/ContextBar";
import { ErrorBlock } from "~/components/chat/ErrorBlock";
import { Markdown } from "~/components/chat/Markdown";
import { PromptGroupProvider } from "~/components/chat/PromptCard";
import { SubagentActivity } from "~/components/chat/SubagentActivity";
import { ThinkingBlock } from "~/components/chat/ThinkingBlock";
import { ThinkingIcon, ToolIcon } from "~/components/chat/ToolIcons";
import { formatSummary, ToolUseBlock } from "~/components/chat/ToolUseBlock";
import { useDebouncedValue } from "~/hooks/useDebouncedValue";
import type {
  ActivityItem,
  ActivitySegment,
  AgentMessageSegment,
  ErrorSegment,
  Segment,
} from "~/lib/segments";
import { segmentKey } from "~/lib/segments";
import type { CompactBoundaryEvent, ToolUseEvent } from "~/stores/chat-store";
import { useChatStore } from "~/stores/chat-store";
import { useStreamingStore } from "~/stores/streaming-store";
import { CollapsibleGroup } from "./CollapsibleGroup";
import { ImageLightbox } from "./ImageLightbox";

const STREAMING_DEBOUNCE_MS = 80;

// --- Helpers ---

function activityTitle(items: ActivityItem[]): string {
  const thoughts = items.filter((i) => i.kind === "thinking").length;
  const tools = items.filter((i) => i.kind === "tool").length;
  const parts: string[] = [];
  if (thoughts > 0) parts.push(`${thoughts} ${thoughts === 1 ? "thought" : "thoughts"}`);
  if (tools > 0) parts.push(`${tools} ${tools === 1 ? "tool call" : "tool calls"}`);
  return parts.join(" and ");
}

// --- In-flight tool components ---

function InFlightToolContent({
  event,
  sessionId,
  projectPath,
  worktreePath,
}: {
  event: ToolUseEvent;
  sessionId: string;
  projectPath?: string;
  worktreePath?: string;
}) {
  const streamingInput = useStreamingStore((s) => s.toolInputs[sessionId]?.[event.toolId]);
  const hasInput = !!event.toolInput;
  const summary = hasInput
    ? formatSummary(event.toolName, event.toolInput, projectPath, worktreePath)
    : "";

  return (
    <>
      <Loader2 className="h-3 w-3 animate-spin shrink-0" />
      <span className="font-medium shrink-0">{event.toolName}</span>
      {hasInput ? (
        <span className="text-muted-foreground-dim truncate min-w-0">{summary}</span>
      ) : streamingInput ? (
        <span className="text-muted-foreground-faint font-mono truncate min-w-0">
          {streamingInput}
        </span>
      ) : null}
    </>
  );
}

export function InFlightToolStatus(props: {
  event: ToolUseEvent;
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

// --- Segment sub-renderers ---

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
                onImageClick={setLightboxSrc}
              />
              {item.taskEvents && item.taskEvents.length > 0 && (
                <SubagentActivity taskEvents={item.taskEvents} />
              )}
            </div>
          ),
        )}
      </CollapsibleGroup>

      <ImageLightbox src={lightboxSrc} onClose={() => setLightboxSrc(null)} />
    </>
  );
});

export const TextSegmentView = memo(function TextSegmentView({
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
  const debouncedContent = useDebouncedValue(content, STREAMING_DEBOUNCE_MS);
  const markdownContent = isStreaming ? debouncedContent : content;

  return (
    <PromptGroupProvider
      content={content}
      projectId={projectId}
      sessionId={sessionId}
      isStreaming={isStreaming}
    >
      <div className="group/msg rounded-lg px-4 py-2 bg-gradient-to-br from-agent/14 to-agent/8 shadow-lg shadow-black/30 border border-agent/15 backdrop-blur-sm">
        <button
          type="button"
          onClick={() => onCopy(content)}
          className="sticky top-2 float-right ml-2 p-1 rounded max-md:opacity-60 opacity-0 group-hover/msg:opacity-100 hover:bg-background/50 text-muted-foreground transition-opacity z-10"
          aria-label="Copy message"
        >
          {copied ? <Check className="h-3.5 w-3.5" /> : <Copy className="h-3.5 w-3.5" />}
        </button>
        <Markdown content={markdownContent} />
        {isStreaming && <TypingCursor />}
      </div>
    </PromptGroupProvider>
  );
});

function TypingCursor() {
  return (
    <span className="inline-flex items-baseline gap-[2px] ml-1 align-baseline" aria-label="Typing">
      <span className="inline-block h-[5px] w-[5px] rounded-full bg-agent/50 animate-[typing-dot_1s_ease-in-out_0ms_infinite]" />
      <span className="inline-block h-[5px] w-[5px] rounded-full bg-agent/50 animate-[typing-dot_1s_ease-in-out_150ms_infinite]" />
      <span className="inline-block h-[5px] w-[5px] rounded-full bg-agent/50 animate-[typing-dot_1s_ease-in-out_300ms_infinite]" />
    </span>
  );
}

function CompactDivider({
  event,
  postTokens,
}: {
  event: CompactBoundaryEvent;
  postTokens?: number;
}) {
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

function AgentMessageWithIcons({ seg }: { seg: AgentMessageSegment }) {
  const senderIcon = useChatStore((s) => s.sessions[seg.senderSessionId]?.meta.icon);
  const targetIcon = useChatStore((s) => s.sessions[seg.targetSessionId]?.meta.icon);
  return (
    <AgentMessage
      direction={seg.direction}
      senderName={seg.senderName}
      senderSessionId={seg.senderSessionId}
      senderIcon={senderIcon}
      targetName={seg.targetName}
      targetSessionId={seg.targetSessionId}
      targetIcon={targetIcon}
      content={seg.content}
      messageType={seg.messageType}
    />
  );
}

// --- Main renderer ---

interface SegmentRendererProps {
  seg: Segment;
  idx: number;
  totalSegments: number;
  isStreaming: boolean;
  showEvents: boolean;
  sessionId: string;
  projectId: string;
  projectPath?: string;
  worktreePath?: string;
  postCompactTokens?: number;
  onCopy: (text: string) => void;
  copied: boolean;
}

export function SegmentRenderer({
  seg,
  idx,
  totalSegments,
  isStreaming,
  showEvents,
  sessionId,
  projectId,
  projectPath,
  worktreePath,
  postCompactTokens,
  onCopy,
  copied,
}: SegmentRendererProps) {
  const key = segmentKey(seg, idx);
  const isLastSegment = isStreaming && idx === totalSegments - 1;

  if (seg.kind === "user_message") return null;

  if (!showEvents && seg.kind === "activity") {
    if (isLastSegment) {
      const toolItems = seg.items.filter(
        (it): it is ActivityItem & { kind: "tool" } => it.kind === "tool",
      );
      const inFlightTool = [...toolItems].reverse().find((t) => !t.result);
      if (inFlightTool) {
        return (
          <InFlightToolStatus
            key={key}
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

  if (seg.kind === "activity") {
    return (
      <ActivitySegmentView
        key={key}
        segment={seg}
        isStreaming={isLastSegment}
        sessionId={sessionId}
        projectPath={projectPath}
        worktreePath={worktreePath}
      />
    );
  }

  if (seg.kind === "text") {
    return (
      <TextSegmentView
        key={key}
        content={seg.content}
        onCopy={onCopy}
        copied={copied}
        projectId={projectId}
        sessionId={sessionId}
        isStreaming={false}
      />
    );
  }

  if (seg.kind === "error") {
    return <ErrorSegmentView key={key} segment={seg} />;
  }

  if (seg.kind === "compact") {
    return <CompactDivider key={key} event={seg.event} postTokens={postCompactTokens} />;
  }

  if (seg.kind === "agent_message") {
    return <AgentMessageWithIcons key={key} seg={seg} />;
  }

  return null;
}
