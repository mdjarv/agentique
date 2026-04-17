import { useNavigate } from "@tanstack/react-router";
import { Hash, Unlink } from "lucide-react";
import { useCallback } from "react";
import { toast } from "sonner";
import {
  ContextMenu,
  ContextMenuContent,
  ContextMenuItem,
  ContextMenuSeparator,
  ContextMenuTrigger,
} from "~/components/ui/context-menu";
import { useWebSocket } from "~/hooks/useWebSocket";
import { dissolveChannel } from "~/lib/channel-actions";
import { getErrorMessage } from "~/lib/utils";
import { useAppStore } from "~/stores/app-store";
import { CompletedSessionsBlock } from "./CompletedSessionsBlock";
import {
  getTodoProgress,
  LeadSessionContent,
  SessionContent,
  WorkerSessionContent,
} from "./SessionRow";
import type { SessionItem } from "./types";
import { SessionSidebarRow } from "./useActiveSessionId";
import { useChannelGroups } from "./useChannelGroups";

export function ChannelSessions({
  sessions,
  onSessionClick,
  projectSlug,
  completed,
  sessionLevel,
  workerLevel,
}: {
  sessions: SessionItem[];
  onSessionClick: (id: string) => void;
  projectSlug: string;
  completed: SessionItem[];
  sessionLevel: number;
  workerLevel: number;
}) {
  const { topLevel, workerMap, channelForLead } = useChannelGroups(sessions);
  const ws = useWebSocket();
  const navigate = useNavigate();

  const handleDissolve = useCallback(
    async (channelId: string) => {
      try {
        await dissolveChannel(ws, channelId);
      } catch (err) {
        toast.error(getErrorMessage(err, "Failed to dissolve channel"));
      }
    },
    [ws],
  );

  const handleViewTimeline = useCallback(
    (channelId: string) => {
      useAppStore.getState().setSidebarOpen(false);
      navigate({
        to: "/project/$projectSlug/channel/$channelId",
        params: { projectSlug, channelId },
      });
    },
    [navigate, projectSlug],
  );

  return (
    <div className="flex flex-col gap-1">
      {topLevel.map(({ id, data }) => {
        const workers = workerMap.get(id);
        const channelId = channelForLead.get(id);

        return (
          <div key={id}>
            {channelId ? (
              <ContextMenu>
                <ContextMenuTrigger asChild>
                  <SessionSidebarRow
                    sessionId={id}
                    indent={sessionLevel}
                    onClick={() => onSessionClick(id)}
                  >
                    <LeadSessionContent data={data} workerCount={workers?.length ?? 0} />
                  </SessionSidebarRow>
                </ContextMenuTrigger>
                <ContextMenuContent>
                  <ContextMenuItem onClick={() => handleViewTimeline(channelId)}>
                    <Hash className="size-3.5" />
                    <span>View timeline</span>
                  </ContextMenuItem>
                  <ContextMenuSeparator />
                  <ContextMenuItem
                    onClick={() => handleDissolve(channelId)}
                    className="text-destructive focus:text-destructive"
                  >
                    <Unlink className="size-3.5" />
                    <span>Dissolve team</span>
                  </ContextMenuItem>
                </ContextMenuContent>
              </ContextMenu>
            ) : (
              <SessionSidebarRow
                sessionId={id}
                indent={sessionLevel}
                onClick={() => onSessionClick(id)}
                todoProgress={getTodoProgress(data)}
              >
                <SessionContent data={data} />
              </SessionSidebarRow>
            )}

            {workers?.map(({ id: wId, data: wData }) => (
              <SessionSidebarRow
                key={wId}
                sessionId={wId}
                indent={workerLevel}
                compact
                onClick={() => onSessionClick(wId)}
              >
                <WorkerSessionContent data={wData} />
              </SessionSidebarRow>
            ))}
          </div>
        );
      })}

      <CompletedSessionsBlock
        completed={completed}
        onSessionClick={onSessionClick}
        sessionLevel={sessionLevel}
      />
    </div>
  );
}
