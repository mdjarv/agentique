import { ArrowDown, ArrowUp, ArrowUpToLine, GitBranch, RefreshCw } from "lucide-react";
import { type ReactNode, useCallback, useState } from "react";
import { toast } from "sonner";
import {
  HoverCard,
  HoverCardArrow,
  HoverCardContent,
  HoverCardTrigger,
} from "~/components/ui/hover-card";
import { useWebSocket } from "~/hooks/useWebSocket";
import { fetchProject, pushProject } from "~/lib/project-actions";
import { cn } from "~/lib/utils";
import { type ProjectGitStatus, useAppStore } from "~/stores/app-store";

interface ProjectHoverCardProps {
  projectId: string;
  gitStatus: ProjectGitStatus | undefined;
  children: ReactNode;
}

export function ProjectHoverCard({ projectId, gitStatus, children }: ProjectHoverCardProps) {
  const ws = useWebSocket();
  const [pushing, setPushing] = useState(false);
  const [fetching, setFetching] = useState(false);

  const handlePush = useCallback(async () => {
    setPushing(true);
    try {
      const status = await pushProject(ws, projectId);
      useAppStore.getState().setProjectGitStatus(status);
      toast.success("Pushed");
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Push failed");
    } finally {
      setPushing(false);
    }
  }, [ws, projectId]);

  const handleFetch = useCallback(async () => {
    setFetching(true);
    try {
      const status = await fetchProject(ws, projectId);
      useAppStore.getState().setProjectGitStatus(status);
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Fetch failed");
    } finally {
      setFetching(false);
    }
  }, [ws, projectId]);

  if (!gitStatus?.branch) return <>{children}</>;

  const ahead = gitStatus.aheadRemote > 0;
  const behind = gitStatus.behindRemote > 0;
  const dirty = gitStatus.uncommittedCount > 0;
  const canPush = ahead && gitStatus.hasRemote;
  const canFetch = gitStatus.hasRemote;

  return (
    <HoverCard openDelay={300} closeDelay={150}>
      <HoverCardTrigger asChild>
        <div>{children}</div>
      </HoverCardTrigger>
      <HoverCardContent side="right" align="start" sideOffset={4} className="w-52 p-0">
        <HoverCardArrow width={10} height={5} />

        {/* Git info header */}
        <div className="px-3 py-2 border-b">
          <div className="flex items-center gap-1.5 text-xs text-muted-foreground">
            <GitBranch className="size-3 shrink-0" />
            <span className="truncate font-mono">{gitStatus.branch}</span>
          </div>
          {(ahead || behind || dirty) && (
            <div className="flex items-center gap-2 mt-1 text-xs text-muted-foreground">
              {dirty && (
                <span className="text-[#e0af68]/80">{gitStatus.uncommittedCount} uncommitted</span>
              )}
              {ahead && (
                <span className="flex items-center gap-0.5">
                  <ArrowUp className="size-2.5" />
                  {gitStatus.aheadRemote} ahead
                </span>
              )}
              {behind && (
                <span className="flex items-center gap-0.5 text-[#7aa2f7]/80">
                  <ArrowDown className="size-2.5" />
                  {gitStatus.behindRemote} behind
                </span>
              )}
            </div>
          )}
        </div>

        {/* Actions */}
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
      </HoverCardContent>
    </HoverCard>
  );
}

function ActionItem({
  icon: Icon,
  label,
  onClick,
  disabled,
}: {
  icon: React.ComponentType<{ className?: string }>;
  label: string;
  onClick: () => void;
  disabled?: boolean;
}) {
  return (
    <button
      type="button"
      disabled={disabled}
      onClick={(e) => {
        e.stopPropagation();
        onClick();
      }}
      className={cn(
        "flex w-full items-center gap-2 px-3 py-1.5 text-sm text-popover-foreground transition-colors cursor-pointer",
        disabled ? "opacity-50 cursor-default" : "hover:bg-accent",
      )}
    >
      <Icon className="size-3.5" />
      {label}
    </button>
  );
}
