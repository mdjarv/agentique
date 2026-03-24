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

interface TurnBlockProps {
  turn: Turn;
  isLast: boolean;
  sessionId: string;
  currentAssistantText: string;
  sessionState: string;
  projectPath?: string;
  worktreePath?: string;
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

export function TurnBlock({
  turn,
  isLast,
  sessionId,
  currentAssistantText,
  sessionState,
  projectPath,
  worktreePath,
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

  const textContent = isStreaming
    ? currentAssistantText
    : turn.events
        .filter((e) => e.type === "text")
        .map((e) => e.content ?? "")
        .join("");

  const thinkingEvents = turn.events.filter((e) => e.type === "thinking");
  const toolUseEvents = turn.events.filter((e) => e.type === "tool_use");
  const toolResultEvents = turn.events.filter((e) => e.type === "tool_result");
  const resultEvent = turn.events.find((e) => e.type === "result");
  const errorEvents = turn.events.filter((e) => e.type === "error");

  const hasAssistantContent =
    textContent ||
    thinkingEvents.length > 0 ||
    toolUseEvents.length > 0 ||
    errorEvents.length > 0 ||
    isStreaming;

  const renderToolPair = (toolUse: ChatEvent) => {
    const result = toolResultEvents.find((r) => r.toolId === toolUse.toolId);
    return (
      <div key={toolUse.id} className="space-y-1">
        <ToolUseBlock
          name={toolUse.toolName ?? "Unknown"}
          input={toolUse.toolInput}
          category={toolUse.category}
          toolId={toolUse.toolId}
          sessionId={sessionId}
          projectPath={projectPath}
          worktreePath={worktreePath}
        />
        {result && <ToolResultBlock content={result.content ?? ""} />}
      </div>
    );
  };

  const renderThinkingBlocks = () => {
    if (thinkingEvents.length === 0) return null;
    if (thinkingEvents.length === 1) {
      return <ThinkingBlock content={thinkingEvents[0]?.content ?? ""} />;
    }
    return (
      <CollapsibleGroup
        label="thinking blocks"
        icon={<Brain className="h-3 w-3" />}
        count={thinkingEvents.length}
        defaultExpanded={false}
      >
        {thinkingEvents.map((e) => (
          <ThinkingBlock key={e.id} content={e.content ?? ""} />
        ))}
      </CollapsibleGroup>
    );
  };

  const renderToolCalls = () => {
    if (toolUseEvents.length === 0) return null;

    const inFlightTool = isStreaming
      ? [...toolUseEvents]
          .reverse()
          .find((tu) => !toolResultEvents.some((r) => r.toolId === tu.toolId))
      : undefined;

    return (
      <CollapsibleGroup
        label={toolUseEvents.length === 1 ? "tool call" : "tool calls"}
        icon={<Terminal className="h-3 w-3" />}
        count={toolUseEvents.length}
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
        {toolUseEvents.map(renderToolPair)}
      </CollapsibleGroup>
    );
  };

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
            {/* Thinking blocks */}
            {renderThinkingBlocks()}

            {/* Tool use/result pairs */}
            {renderToolCalls()}

            {/* Streaming indicator */}
            {isStreaming &&
              !textContent &&
              thinkingEvents.length === 0 &&
              toolUseEvents.length === 0 && (
                <div className="flex items-center gap-2 text-muted-foreground text-sm px-1">
                  <Loader2 className="h-3.5 w-3.5 animate-spin" />
                  <span>{sessionState === "running" ? "Working..." : "Connecting..."}</span>
                </div>
              )}

            {isStreaming &&
              (toolUseEvents.length > 0 || thinkingEvents.length > 0) &&
              !textContent && (
                <div className="flex items-center gap-2 text-muted-foreground/60 text-xs px-1">
                  <Loader2 className="h-3 w-3 animate-spin" />
                </div>
              )}

            {/* Text content */}
            {textContent && (
              <div className="relative group/msg rounded-lg px-4 py-2 bg-muted">
                <button
                  type="button"
                  onClick={() => handleCopy(textContent)}
                  className="absolute top-2 right-2 p-1 rounded opacity-0 group-hover/msg:opacity-100 hover:bg-background/50 text-muted-foreground transition-opacity"
                  aria-label="Copy message"
                >
                  {copied ? <Check className="h-3.5 w-3.5" /> : <Copy className="h-3.5 w-3.5" />}
                </button>
                <Markdown content={textContent} />
              </div>
            )}

            {/* Streaming indicator after text while still working */}
            {isStreaming && textContent && (
              <div className="flex items-center gap-2 text-muted-foreground/60 text-xs px-1">
                <Loader2 className="h-3 w-3 animate-spin" />
              </div>
            )}

            {/* Error events */}
            {errorEvents.map((e) => (
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

            {/* Result metadata — duration only */}
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
