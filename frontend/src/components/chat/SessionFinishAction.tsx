import { Check, CheckCircle2 } from "lucide-react";
import { MergeDropdown } from "~/components/chat/MergeDropdown";
import { Button } from "~/components/ui/button";
import type { useGitActions } from "~/hooks/git/useGitActions";
import type { ProjectGitStatus } from "~/stores/app-store";
import type { SessionMetadata } from "~/stores/chat-store";

type GitActions = ReturnType<typeof useGitActions>;

export type FinishActionKind = "merge" | "merged" | "markDone" | null;

/**
 * Which finishing control (if any) applies to a session in its current state.
 * Kept as a pure function so the mobile tab strip can decide whether to render
 * the row at all without duplicating the merge-eligibility logic — it mirrors
 * the `canMerge` computation in {@link SessionHeader}.
 */
export function finishActionKind(meta: SessionMetadata, git?: GitActions): FinishActionKind {
  const isWorktree = !!meta.worktreeBranch;
  const isBusy = meta.state === "running" || meta.state === "merging";
  const ahead = meta.commitsAhead ?? 0;
  const behind = meta.commitsBehind ?? 0;
  const isMerged = !!meta.worktreeMerged && ahead === 0 && behind === 0;

  if (git && isWorktree && !meta.branchMissing && !isMerged && ahead > 0 && !isBusy) return "merge";
  if (isWorktree && isMerged) return "merged";
  if (meta.state === "idle" || meta.state === "stopped" || meta.state === "failed")
    return "markDone";
  return null;
}

interface SessionFinishActionProps {
  meta: SessionMetadata;
  git?: GitActions;
  projectGitStatus?: ProjectGitStatus;
  onMarkDone: () => void;
}

/**
 * The mobile "finish the session" control: a single, state-aware slot that lives
 * on the tab strip. It shows the right verb for where the session is — merge
 * when there are commits ahead, mark-done when there's nothing to merge, a quiet
 * confirmation once merged — and nothing while the session is busy (the composer
 * already owns Stop mid-turn). Desktop keeps its own inline controls in the header.
 */
export function SessionFinishAction({
  meta,
  git,
  projectGitStatus,
  onMarkDone,
}: SessionFinishActionProps) {
  const kind = finishActionKind(meta, git);

  if (kind === "merge" && git) {
    const projectDirty = !!projectGitStatus && projectGitStatus.uncommittedCount > 0;
    return (
      <MergeDropdown
        git={git}
        projectDirty={projectDirty}
        className="h-7 gap-1 rounded-md border border-success/40 bg-success/15 px-2.5 text-xs text-success hover:bg-success/25"
      />
    );
  }

  if (kind === "merged") {
    return (
      <span className="inline-flex h-7 shrink-0 items-center gap-1 px-2 text-xs text-muted-foreground">
        <CheckCircle2 className="size-3.5 text-success/70" />
        Merged
      </span>
    );
  }

  if (kind === "markDone") {
    return (
      <Button
        variant="ghost"
        size="sm"
        className="h-7 shrink-0 gap-1 rounded-md border border-success/40 px-2.5 text-xs text-success hover:bg-success/10"
        title="Mark session done"
        onClick={onMarkDone}
      >
        <Check className="size-3.5" />
        Mark done
      </Button>
    );
  }

  return null;
}
