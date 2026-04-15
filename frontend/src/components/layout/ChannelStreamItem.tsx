import { useDroppable } from "@dnd-kit/core";
import { useNavigate } from "@tanstack/react-router";
import { ChevronDown, ChevronRight, Hash } from "lucide-react";
import { memo, useCallback, useState } from "react";
import { useShallow } from "zustand/shallow";
import {
  ContextMenu,
  ContextMenuContent,
  ContextMenuItem,
  ContextMenuTrigger,
} from "~/components/ui/context-menu";
import { useWebSocket } from "~/hooks/useWebSocket";
import { dissolveChannel } from "~/lib/channel-actions";
import { cn, getErrorMessage } from "~/lib/utils";
import { useAppStore } from "~/stores/app-store";
import { useChannelStore } from "~/stores/channel-store";
import { useChatStore } from "~/stores/chat-store";
import { StreamSessionRow } from "./session/StreamSessionRow";

interface ChannelStreamItemProps {
  channelId: string;
  projectSlug: string;
  activeSessionId: string | undefined;
  isDragActive: boolean;
  onSessionClick: (sessionId: string) => void;
  hideProjectPill?: boolean;
}

const EMPTY_MEMBERS: string[] = [];

export const ChannelStreamItem = memo(function ChannelStreamItem({
  channelId,
  projectSlug,
  activeSessionId,
  isDragActive,
  onSessionClick,
  hideProjectPill,
}: ChannelStreamItemProps) {
  const navigate = useNavigate();
  const ws = useWebSocket();
  const [expanded, setExpanded] = useState(false);

  const channel = useChannelStore((s) => s.channels[channelId]);
  const leadName = useChannelStore(
    (s) => s.channels[channelId]?.members.find((m) => m.role === "lead")?.name ?? null,
  );
  const memberSessionIds = useChannelStore(
    useShallow((s) => {
      const ch = s.channels[channelId];
      if (!ch) return EMPTY_MEMBERS;
      // Sort lead first
      return [...ch.members]
        .sort((a, b) => (a.role === "lead" ? -1 : b.role === "lead" ? 1 : 0))
        .map((m) => m.sessionId);
    }),
  );

  const { setNodeRef, isOver } = useDroppable({
    id: `channel:${channelId}`,
    data: { channelId },
  });

  // Status dots: summarize member states
  const statusCounts = useChatStore(
    useShallow((s) => {
      let running = 0;
      let pending = 0;
      let failed = 0;

      for (const id of memberSessionIds) {
        const data = s.sessions[id];
        if (!data) continue;
        if (data.pendingApproval || data.pendingQuestion) pending++;
        else if (data.meta.state === "running") running++;
        else if (data.meta.state === "failed") failed++;
      }

      return { running, pending, failed };
    }),
  );

  const handleNameClick = useCallback(
    (e: React.MouseEvent) => {
      e.stopPropagation();
      useAppStore.getState().setSidebarOpen(false);
      navigate({
        to: "/project/$projectSlug/channel/$channelId",
        params: { projectSlug, channelId },
      });
    },
    [navigate, projectSlug, channelId],
  );

  const handleDissolve = useCallback(async () => {
    try {
      await dissolveChannel(ws, channelId);
    } catch (err) {
      const { toast } = await import("sonner");
      toast.error(getErrorMessage(err, "Failed to dissolve channel"));
    }
  }, [ws, channelId]);

  if (!channel) return null;

  return (
    <ContextMenu>
      <ContextMenuTrigger asChild>
        <div className="mt-0.5">
          <button
            ref={setNodeRef}
            type="button"
            onClick={() => setExpanded((v) => !v)}
            className={cn(
              "group flex w-full items-center gap-1.5 rounded-md px-2 py-1.5 text-left cursor-pointer",
              "bg-primary/[0.03] hover:bg-primary/[0.07] transition-colors",
              "border border-primary/10",
              isOver && "bg-primary/10 ring-1 ring-primary/30",
            )}
          >
            {expanded ? (
              <ChevronDown className="size-3 shrink-0 text-primary/50" />
            ) : (
              <ChevronRight className="size-3 shrink-0 text-primary/50" />
            )}
            <Hash className="size-3.5 shrink-0 text-primary/60" />
            <span
              onClick={handleNameClick}
              onKeyDown={(e) => {
                if (e.key === "Enter") handleNameClick(e as unknown as React.MouseEvent);
              }}
              className="text-sm font-medium text-sidebar-foreground/80 truncate hover:text-sidebar-foreground hover:underline"
            >
              {channel.name}
            </span>
            {leadName && (
              <span
                className="min-w-0 max-w-[40%] truncate text-[10px] text-muted-foreground-faint"
                title={`Lead: ${leadName}`}
              >
                {leadName}
              </span>
            )}

            {/* Status dots */}
            <span className="ml-auto flex items-center gap-1 shrink-0">
              {statusCounts.pending > 0 && (
                <span
                  className="inline-block h-1.5 w-1.5 rounded-full bg-orange animate-pulse"
                  title={`${statusCounts.pending} awaiting input`}
                />
              )}
              {statusCounts.running > 0 && (
                <span
                  className="inline-block h-1.5 w-1.5 rounded-full bg-teal animate-pulse"
                  title={`${statusCounts.running} running`}
                />
              )}
              {statusCounts.failed > 0 && (
                <span
                  className="inline-block h-1.5 w-1.5 rounded-full bg-destructive"
                  title={`${statusCounts.failed} failed`}
                />
              )}
              <span className="text-xs text-muted-foreground-faint">{memberSessionIds.length}</span>
            </span>
          </button>

          {expanded && (
            <div className="ml-5 mt-0.5">
              {memberSessionIds.length === 0 && (
                <div className="px-2 py-1 text-xs text-muted-foreground-faint italic">
                  Drag sessions here
                </div>
              )}
              {memberSessionIds.map((id) => (
                <StreamSessionRow
                  key={id}
                  sessionId={id}
                  projectSlug={projectSlug}
                  activeSessionId={activeSessionId}
                  isDragActive={isDragActive}
                  onSessionClick={onSessionClick}
                  hideProjectPill={hideProjectPill}
                />
              ))}
            </div>
          )}
        </div>
      </ContextMenuTrigger>
      <ContextMenuContent>
        <ContextMenuItem onClick={handleNameClick}>View timeline</ContextMenuItem>
        <ContextMenuItem onClick={handleDissolve} className="text-destructive">
          Dissolve channel
        </ContextMenuItem>
      </ContextMenuContent>
    </ContextMenu>
  );
});
