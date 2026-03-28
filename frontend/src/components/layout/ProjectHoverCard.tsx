import { ArrowDown, ArrowUp, ArrowUpToLine, GitBranch, RefreshCw } from "lucide-react";
import { type ReactNode, useCallback, useState } from "react";
import { toast } from "sonner";
import { useShallow } from "zustand/shallow";
import {
  HoverCard,
  HoverCardArrow,
  HoverCardContent,
  HoverCardTrigger,
} from "~/components/ui/hover-card";
import { useWebSocket } from "~/hooks/useWebSocket";
import { createTag, fetchProject, pushProject, setProjectTags } from "~/lib/project-actions";
import { getErrorMessage } from "~/lib/utils";
import { type ProjectGitStatus, useAppStore } from "~/stores/app-store";
import { ActionItem } from "./ActionItem";
import { TagInput, nextTagColor } from "./TagInput";

interface ProjectHoverCardProps {
  projectId: string;
  projectPath: string;
  gitStatus: ProjectGitStatus | undefined;
  children: ReactNode;
}

function truncatePath(path: string): string {
  return path.replace(/^\/home\/[^/]+/, "~").replace(/^\/Users\/[^/]+/, "~");
}

export function ProjectHoverCard({
  projectId,
  projectPath,
  gitStatus,
  children,
}: ProjectHoverCardProps) {
  const ws = useWebSocket();
  const [pushing, setPushing] = useState(false);
  const [fetching, setFetching] = useState(false);
  const projectTagIds = useAppStore(
    useShallow((s) =>
      s.projectTags.filter((pt) => pt.project_id === projectId).map((pt) => pt.tag_id),
    ),
  );

  const handlePush = useCallback(async () => {
    setPushing(true);
    try {
      const status = await pushProject(ws, projectId);
      useAppStore.getState().setProjectGitStatus(status);
      toast.success("Pushed");
    } catch (err) {
      toast.error(getErrorMessage(err, "Push failed"));
    } finally {
      setPushing(false);
    }
  }, [ws, projectId]);

  const handleAssignTag = useCallback(
    async (tagId: string) => {
      const newTagIds = [...projectTagIds, tagId];
      try {
        await setProjectTags(ws, projectId, newTagIds);
        useAppStore.getState().setTagsForProject(projectId, newTagIds);
      } catch (err) {
        toast.error(getErrorMessage(err, "Failed to add tag"));
      }
    },
    [ws, projectId, projectTagIds],
  );

  const handleUnassignTag = useCallback(
    async (tagId: string) => {
      const newTagIds = projectTagIds.filter((id) => id !== tagId);
      try {
        await setProjectTags(ws, projectId, newTagIds);
        useAppStore.getState().setTagsForProject(projectId, newTagIds);
      } catch (err) {
        toast.error(getErrorMessage(err, "Failed to remove tag"));
      }
    },
    [ws, projectId, projectTagIds],
  );

  const handleCreateTag = useCallback(
    async (name: string) => {
      const color = nextTagColor(useAppStore.getState().tags.length);
      const tag = await createTag(ws, name, color);
      useAppStore.getState().addTag(tag);
      return tag;
    },
    [ws],
  );

  const handleFetch = useCallback(async () => {
    setFetching(true);
    try {
      const status = await fetchProject(ws, projectId);
      useAppStore.getState().setProjectGitStatus(status);
    } catch (err) {
      toast.error(getErrorMessage(err, "Fetch failed"));
    } finally {
      setFetching(false);
    }
  }, [ws, projectId]);

  const ahead = gitStatus && gitStatus.aheadRemote > 0;
  const behind = gitStatus && gitStatus.behindRemote > 0;
  const dirty = gitStatus && gitStatus.uncommittedCount > 0;
  const canPush = ahead && gitStatus.hasRemote;
  const canFetch = gitStatus?.hasRemote;

  return (
    <HoverCard openDelay={300} closeDelay={150}>
      <HoverCardTrigger asChild>
        <div>{children}</div>
      </HoverCardTrigger>
      <HoverCardContent side="right" align="start" sideOffset={4} className="w-52 p-0">
        <HoverCardArrow width={10} height={5} />

        {/* Path */}
        <div className="px-3 py-2 border-b">
          <span className="text-xs text-muted-foreground truncate block">
            {truncatePath(projectPath)}
          </span>
        </div>

        {/* Git info */}
        {gitStatus?.branch && (
          <div className="px-3 py-2 border-b">
            <div className="flex items-center gap-1.5 text-xs text-muted-foreground">
              <GitBranch className="size-3 shrink-0" />
              <span className="truncate font-mono">{gitStatus.branch}</span>
            </div>
            {(ahead || behind || dirty) && (
              <div className="flex items-center gap-2 mt-1 text-xs text-muted-foreground">
                {dirty && (
                  <span className="text-warning/80">{gitStatus.uncommittedCount} uncommitted</span>
                )}
                {ahead && (
                  <span className="flex items-center gap-0.5">
                    <ArrowUp className="size-2.5" />
                    {gitStatus.aheadRemote} ahead
                  </span>
                )}
                {behind && (
                  <span className="flex items-center gap-0.5 text-primary/80">
                    <ArrowDown className="size-2.5" />
                    {gitStatus.behindRemote} behind
                  </span>
                )}
              </div>
            )}
          </div>
        )}

        {/* Tags */}
        <TagInput
          assignedTagIds={projectTagIds}
          onAssign={handleAssignTag}
          onUnassign={handleUnassignTag}
          onCreate={handleCreateTag}
        />

        {/* Actions */}
        {(canPush || canFetch) && (
          <div className="py-1">
            {canPush && (
              <ActionItem
                icon={ArrowUpToLine}
                label={pushing ? "Pushing..." : "Push"}
                onClick={handlePush}
                disabled={pushing}
              />
            )}
            {canFetch && (
              <ActionItem
                icon={RefreshCw}
                label={fetching ? "Fetching..." : "Fetch"}
                onClick={handleFetch}
                disabled={fetching}
              />
            )}
          </div>
        )}
      </HoverCardContent>
    </HoverCard>
  );
}
