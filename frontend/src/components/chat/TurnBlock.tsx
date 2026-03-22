import { Bot, Loader2, User } from "lucide-react";
import { Markdown } from "~/components/chat/Markdown";
import { ThinkingBlock } from "~/components/chat/ThinkingBlock";
import { ToolResultBlock } from "~/components/chat/ToolResultBlock";
import { ToolUseBlock } from "~/components/chat/ToolUseBlock";
import { Avatar, AvatarFallback } from "~/components/ui/avatar";
import type { Turn } from "~/stores/chat-store";
import { useChatStore } from "~/stores/chat-store";

interface TurnBlockProps {
  turn: Turn;
  isLast: boolean;
}

export function TurnBlock({ turn, isLast }: TurnBlockProps) {
  const currentAssistantText = useChatStore((s) => s.currentAssistantText);
  const sessionState = useChatStore((s) => s.sessionState);

  const isStreaming = isLast && !turn.complete;

  // Accumulate text from completed events, or use streaming text for the last turn.
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
          <p className="text-sm whitespace-pre-wrap">{turn.prompt}</p>
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
          <div className="flex-1 space-y-2 max-w-[85%]">
            {/* Thinking blocks */}
            {thinkingEvents.map((e) => (
              <ThinkingBlock key={e.id} content={e.content ?? ""} />
            ))}

            {/* Text content */}
            {textContent && (
              <div className="rounded-lg px-4 py-2 bg-muted">
                <Markdown content={textContent} />
              </div>
            )}

            {/* Streaming indicator when waiting for first content */}
            {isStreaming &&
              !textContent &&
              thinkingEvents.length === 0 &&
              toolUseEvents.length === 0 && (
                <div className="flex items-center gap-2 text-muted-foreground text-sm px-1">
                  <Loader2 className="h-3.5 w-3.5 animate-spin" />
                  <span>{sessionState === "running" ? "Working..." : "Connecting..."}</span>
                </div>
              )}

            {/* Tool use/result pairs */}
            {toolUseEvents.map((toolUse) => {
              const result = toolResultEvents.find((r) => r.toolId === toolUse.toolId);
              return (
                <div key={toolUse.id} className="space-y-1">
                  <ToolUseBlock name={toolUse.toolName ?? "Unknown"} input={toolUse.toolInput} />
                  {result && <ToolResultBlock content={result.content ?? ""} />}
                </div>
              );
            })}

            {/* Streaming indicator after tool calls while waiting for more */}
            {isStreaming && (toolUseEvents.length > 0 || textContent) && (
              <div className="flex items-center gap-2 text-muted-foreground/60 text-xs px-1">
                <Loader2 className="h-3 w-3 animate-spin" />
              </div>
            )}

            {/* Error events */}
            {errorEvents.map((e) => (
              <div
                key={e.id}
                className="rounded-lg px-4 py-2 bg-destructive/10 text-destructive text-sm"
              >
                {e.content}
              </div>
            ))}

            {/* Result metadata */}
            {resultEvent && (
              <div className="text-xs text-muted-foreground flex gap-3">
                {resultEvent.cost != null && resultEvent.cost > 0 && (
                  <span>Cost: ${resultEvent.cost.toFixed(4)}</span>
                )}
                {resultEvent.duration != null && resultEvent.duration > 0 && (
                  <span>{(resultEvent.duration / 1000).toFixed(1)}s</span>
                )}
              </div>
            )}
          </div>
        </div>
      )}
    </div>
  );
}
