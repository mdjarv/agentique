import { Bot } from "lucide-react";
import { memo, type ReactNode, useEffect, useMemo } from "react";
import { useShallow } from "zustand/shallow";
import { UserMessage } from "~/components/chat/UserMessage";
import { Avatar, AvatarFallback } from "~/components/ui/avatar";
import { ANIMATE_CHAT, useAutoAnimate } from "~/hooks/useAutoAnimate";
import { useCopyToClipboard } from "~/hooks/useCopyToClipboard";
import { useProjectIcon } from "~/hooks/useProjectIcon";
import { useTheme } from "~/hooks/useTheme";
import { getProjectColor } from "~/lib/project-colors";
import type { Segment } from "~/lib/segments";
import { buildSegments, buildTurnSections, segmentKey } from "~/lib/segments";
import { useAppStore } from "~/stores/app-store";
import type { ChatEvent, Turn } from "~/stores/chat-store";
import { useChatStore } from "~/stores/chat-store";
import { useStreamingStore } from "~/stores/streaming-store";
import { SegmentRenderer, TextSegmentView } from "./SegmentRenderer";

const EMPTY_STREAMING: ChatEvent[] = [];

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

function AgentAvatar({
  icon: ProjectIcon,
  spinning,
  accentColor,
}: {
  icon: React.ComponentType<{ className?: string }> | null;
  spinning: boolean;
  accentColor?: string;
}) {
  return (
    <div className="shrink-0 self-end relative h-8 w-8 max-md:h-6 max-md:w-6">
      {spinning && (
        <div
          className="absolute inset-[-3px] rounded-full animate-spin [animation-duration:1.2s] [mask:radial-gradient(farthest-side,transparent_calc(100%-2.5px),#000_calc(100%-2px))]"
          style={{
            background: `conic-gradient(from 0deg, transparent 60%, ${accentColor ?? "var(--color-agent)"} 100%)`,
          }}
        />
      )}
      <Avatar className="h-full w-full">
        <AvatarFallback className="bg-agent/15 text-agent">
          {ProjectIcon ? (
            <ProjectIcon className="h-4 w-4 max-md:h-3 max-md:w-3" />
          ) : (
            <Bot className="h-4 w-4 max-md:h-3 max-md:w-3" />
          )}
        </AvatarFallback>
      </Avatar>
    </div>
  );
}

function AgentSectionContent({ className, children }: { className: string; children: ReactNode }) {
  return <div className={className}>{children}</div>;
}

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
  const [outerAnimateRef, setOuterAnimateEnabled] = useAutoAnimate<HTMLDivElement>(ANIMATE_CHAT);
  const isStreaming = isLast && !turn.complete;

  useEffect(() => {
    setOuterAnimateEnabled(!isStreaming);
  }, [isStreaming, setOuterAnimateEnabled]);
  const project = useAppStore((s) => s.projects.find((p) => p.id === projectId));
  const projectIds = useAppStore(useShallow((s) => s.projects.map((p) => p.id)));
  const { resolvedTheme } = useTheme();
  const projectAccentColor = useMemo(
    () =>
      project
        ? getProjectColor(project.color, project.id, projectIds, resolvedTheme).fg
        : undefined,
    [project, projectIds, resolvedTheme],
  );
  const ProjectIcon = useProjectIcon(project?.icon ?? "");

  const currentAssistantText = useStreamingStore((s) =>
    isStreaming ? (s.texts[sessionId] ?? "") : "",
  );

  const streamingEvents = useChatStore((s) =>
    isStreaming ? (s.sessions[sessionId]?.streamingEvents ?? EMPTY_STREAMING) : EMPTY_STREAMING,
  );

  const allEvents = useMemo(
    () => (streamingEvents.length > 0 ? [...turn.events, ...streamingEvents] : turn.events),
    [turn.events, streamingEvents],
  );

  const { segments, resultEvent } = useMemo(
    () => buildSegments(allEvents, turn.complete),
    [allEvents, turn.complete],
  );

  const committedText = useMemo(
    () =>
      allEvents
        .filter((e) => e.type === "text")
        .map((e) => e.content ?? "")
        .join("\n\n"),
    [allEvents],
  );

  const streamingTail = useMemo(() => {
    if (!isStreaming || !currentAssistantText) return "";
    if (committedText && currentAssistantText.startsWith(committedText)) {
      return currentAssistantText.slice(committedText.length).replace(/^\n\n/, "");
    }
    if (!committedText) return currentAssistantText;
    return "";
  }, [isStreaming, currentAssistantText, committedText]);

  const visibleSegmentCount = showEvents
    ? segments.length
    : segments.filter((s) => s.kind !== "activity").length;
  const hasAssistantContent = visibleSegmentCount > 0 || streamingTail || isStreaming;

  const turnSections = useMemo(() => buildTurnSections(segments), [segments]);

  const hasTrailingContent =
    streamingTail || isStreaming || (resultEvent?.duration != null && resultEvent.duration > 0);
  const lastIsAgent =
    turnSections.length > 0 && turnSections[turnSections.length - 1]?.kind === "agent";
  const renderSections =
    hasTrailingContent && !lastIsAgent
      ? [...turnSections, { kind: "agent" as const, items: [] as { seg: Segment; idx: number }[] }]
      : turnSections;

  let lastAgentIdx = -1;
  for (let i = renderSections.length - 1; i >= 0; i--) {
    if (renderSections[i]?.kind === "agent") {
      lastAgentIdx = i;
      break;
    }
  }

  return (
    <div ref={outerAnimateRef} className="space-y-4">
      <UserMessage prompt={turn.prompt} attachments={turn.attachments} />

      {hasAssistantContent &&
        renderSections.map((section, si) => {
          if (section.kind === "user") {
            return (
              <UserMessage
                key={`usermsg-${section.idx}`}
                prompt={section.seg.content}
                attachments={section.seg.attachments}
                deliveryStatus={section.seg.deliveryStatus}
              />
            );
          }

          const isLastSection = si === renderSections.length - 1;
          const sectionKey = `agent-${section.items[0]?.idx ?? "tail"}`;
          return (
            <div key={sectionKey} className="space-y-1">
              <div className="flex gap-3 max-md:gap-2">
                {si === lastAgentIdx ? (
                  <AgentAvatar
                    icon={ProjectIcon ?? null}
                    spinning={isStreaming && isLastSection}
                    accentColor={projectAccentColor}
                  />
                ) : (
                  <div className="w-8 shrink-0 max-md:hidden" />
                )}
                <AgentSectionContent className="flex-1 space-y-3 max-w-[85%] max-md:max-w-full min-w-0 overflow-x-clip pr-2 max-md:pr-0">
                  {section.items.map(({ seg, idx }) => (
                    <SegmentRenderer
                      key={segmentKey(seg, idx)}
                      seg={seg}
                      idx={idx}
                      totalSegments={segments.length}
                      isStreaming={isStreaming}
                      showEvents={showEvents}
                      sessionId={sessionId}
                      projectId={projectId}
                      projectPath={projectPath}
                      worktreePath={worktreePath}
                      postCompactTokens={postCompactTokens}
                      onCopy={handleCopy}
                      copied={copied}
                    />
                  ))}

                  {isLastSection && streamingTail && (
                    <TextSegmentView
                      content={streamingTail}
                      onCopy={handleCopy}
                      copied={copied}
                      projectId={projectId}
                      sessionId={sessionId}
                      isStreaming
                    />
                  )}

                  {isLastSection && isStreaming && segments.length === 0 && !streamingTail && (
                    <span className="text-muted-foreground text-sm px-1">
                      {sessionState === "running" ? "Working..." : "Connecting..."}
                    </span>
                  )}
                </AgentSectionContent>
              </div>
              {isLastSection && resultEvent?.duration != null && resultEvent.duration > 0 && (
                <div className="text-xs text-muted-foreground flex items-center gap-1.5 ml-11 max-md:ml-0">
                  {resultEvent.timestamp && (
                    <span>
                      {new Date(resultEvent.timestamp).toLocaleTimeString([], {
                        hour: "2-digit",
                        minute: "2-digit",
                      })}
                    </span>
                  )}
                  {resultEvent.timestamp && <span className="text-muted-foreground/40">·</span>}
                  <span>{(resultEvent.duration / 1000).toFixed(1)}s</span>
                </div>
              )}
            </div>
          );
        })}
    </div>
  );
});
