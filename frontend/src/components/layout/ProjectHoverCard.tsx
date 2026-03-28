import { ArrowDown, ArrowUp, ArrowUpToLine, Check, GitBranch, RefreshCw } from "lucide-react";
import { type ReactNode, useCallback, useState } from "react";
import { toast } from "sonner";
import {
  HoverCard,
  HoverCardArrow,
  HoverCardContent,
  HoverCardTrigger,
} from "~/components/ui/hover-card";
import { useWebSocket } from "~/hooks/useWebSocket";
import { fetchProject, pushProject, setProjectTags } from "~/lib/project-actions";
import { getTagColor } from "~/lib/tag-colors";
import { cn, getErrorMessage } from "~/lib/utils";
import { type ProjectGitStatus, useAppStore } from "~/stores/app-store";
import { ActionItem } from "./ActionItem";

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
  const tags = useAppStore((s) => s.tags);
  const projectTagIds = useAppStore((s) =>
    s.projectTags.filter((pt) => pt.project_id === projectId).map((pt) => pt.tag_id),
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

  const handleToggleTag = useCallback(
    async (tagId: string) => {
      const newTagIds = projectTagIds.includes(tagId)
        ? projectTagIds.filter((id) => id !== tagId)
        : [...projectTagIds, tagId];
      try {
        await setProjectTags(ws, projectId, newTagIds);
        useAppStore.getState().setTagsForProject(projectId, newTagIds);
      } catch (err) {
        toast.error(getErrorMessage(err, "Failed to update tags"));
      }
    },
    [ws, projectId, projectTagIds],
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
        {tags.length > 0 && (
          <div className="px-3 py-2 border-b">
            <div className="text-xs font-semibold text-muted-foreground mb-1.5">Tags</div>
            <div className="space-y-0.5">
              {tags.map((tag) => {
                const color = getTagColor(tag.color);
                const isAssigned = projectTagIds.includes(tag.id);
                return (
                  <button
                    key={tag.id}
                    type="button"
                    onClick={() => handleToggleTag(tag.id)}
                    className={cn(
                      "flex items-center gap-2 w-full px-1.5 py-1 rounded text-xs hover:bg-accent transition-colors cursor-pointer",
                      isAssigned && "text-foreground-bright",
                    )}
                  >
                    <span
                      className="inline-block size-2.5 rounded-full shrink-0"
                      style={{ backgroundColor: color.bg }}
                    />
                    <span className="flex-1 text-left truncate">{tag.name}</span>
                    {isAssigned && <Check className="size-3 text-success shrink-0" />}
                  </button>
                );
              })}
            </div>
          </div>
        )}

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
