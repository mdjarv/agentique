import {
  ArrowDown,
  ArrowDownToLine,
  ArrowUp,
  ArrowUpToLine,
  Check,
  GitCommitHorizontal,
  Loader2,
  RefreshCw,
} from "lucide-react";
import { type ReactNode, useCallback, useState } from "react";
import { toast } from "sonner";
import { Button } from "~/components/ui/button";
import {
  HoverCard,
  HoverCardArrow,
  HoverCardContent,
  HoverCardTrigger,
} from "~/components/ui/hover-card";
import { Input } from "~/components/ui/input";
import { useWebSocket } from "~/hooks/useWebSocket";
import {
  commitProject,
  fetchProject,
  getProjectGitStatus,
  pullProject,
  pushProject,
} from "~/lib/project-actions";
import { getErrorMessage } from "~/lib/utils";
import { type ProjectGitStatus, useAppStore } from "~/stores/app-store";
import { ActionItem } from "../ActionItem";
import { BranchSelector } from "../git/BranchSelector";

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
  const [pulling, setPulling] = useState(false);
  const [committing, setCommitting] = useState(false);
  const [commitOpen, setCommitOpen] = useState(false);
  const [commitMessage, setCommitMessage] = useState("");

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

  const handlePull = useCallback(async () => {
    setPulling(true);
    try {
      const status = await pullProject(ws, projectId);
      useAppStore.getState().setProjectGitStatus(status);
      toast.success("Pulled");
    } catch (err) {
      toast.error(getErrorMessage(err, "Pull failed"));
    } finally {
      setPulling(false);
    }
  }, [ws, projectId]);

  const handleCommit = useCallback(
    async (message: string) => {
      setCommitting(true);
      try {
        await commitProject(ws, projectId, message);
        const status = await getProjectGitStatus(ws, projectId);
        useAppStore.getState().setProjectGitStatus(status);
        toast.success("Committed");
      } catch (err) {
        toast.error(getErrorMessage(err, "Commit failed"));
        throw err;
      } finally {
        setCommitting(false);
      }
    },
    [ws, projectId],
  );

  const handleCommitSubmit = useCallback(
    async (e: React.FormEvent) => {
      e.preventDefault();
      const trimmed = commitMessage.trim();
      if (!trimmed || committing) return;
      try {
        await handleCommit(trimmed);
        setCommitMessage("");
        setCommitOpen(false);
      } catch {
        // Error already toasted — keep form open
      }
    },
    [commitMessage, committing, handleCommit],
  );

  const handleBranchChanged = useCallback((status: ProjectGitStatus) => {
    useAppStore.getState().setProjectGitStatus(status);
  }, []);

  const ahead = gitStatus && gitStatus.aheadRemote > 0;
  const behind = gitStatus && gitStatus.behindRemote > 0;
  const dirty = gitStatus && gitStatus.uncommittedCount > 0;
  const canPush = ahead && gitStatus.hasRemote;
  const canPull = behind && gitStatus?.hasRemote;
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
            <BranchSelector
              projectId={projectId}
              currentBranch={gitStatus.branch}
              isDirty={!!dirty}
              onBranchChanged={handleBranchChanged}
            />
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

        {/* Actions */}
        {(canPush || canPull || canFetch || dirty) && (
          <div className="py-1">
            {dirty &&
              (commitOpen ? (
                <form onSubmit={handleCommitSubmit} className="flex items-center gap-1.5 px-3 py-1">
                  <Input
                    value={commitMessage}
                    onChange={(e) => setCommitMessage(e.target.value)}
                    placeholder="Commit message..."
                    className="h-7 text-xs"
                    autoFocus
                    disabled={committing}
                  />
                  <Button
                    type="submit"
                    size="icon-xs"
                    disabled={committing || !commitMessage.trim()}
                  >
                    {committing ? (
                      <Loader2 className="size-3 animate-spin" />
                    ) : (
                      <Check className="size-3" />
                    )}
                  </Button>
                </form>
              ) : (
                <ActionItem
                  icon={GitCommitHorizontal}
                  label={`Commit ${gitStatus.uncommittedCount} files`}
                  onClick={() => setCommitOpen(true)}
                />
              ))}
            {canPull && (
              <ActionItem
                icon={ArrowDownToLine}
                label={pulling ? "Pulling..." : "Pull"}
                onClick={handlePull}
                disabled={pulling}
              />
            )}
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
