import { Archive, ArchiveRestore, CheckCircle2 } from "lucide-react";
import { BranchSyncControl } from "~/components/chat/BranchSyncControl";
import { Button } from "~/components/ui/button";
import type { useGitActions } from "~/hooks/git/useGitActions";
import { hasBranchSync } from "~/lib/session/branch-sync";
import type { ProjectGitStatus } from "~/stores/app-store";
import type { SessionMetadata } from "~/stores/chat-store";

type GitActions = ReturnType<typeof useGitActions>;

export type FinishActionKind = "branch" | "merged" | "archive" | null;

/**
 * Which finishing control (if any) applies to a session in its current state.
 * Kept as a pure function so the mobile tab strip can decide whether to render
 * the row at all without duplicating the logic.
 *
 * `"branch"` defers entirely to {@link branchSync} — the header and this strip
 * carry the *same* control, so the eligibility rule lives in one place rather
 * than being mirrored here and drifting, which is exactly what happened between
 * this file and `GitStatusBar` before.
 */
export function finishActionKind(meta: SessionMetadata, git?: GitActions): FinishActionKind {
  if (hasBranchSync(meta, !!git)) return "branch";
  if (meta.worktreeBranch && meta.worktreeMerged) return "merged";
  // Archive is offered only off a settled session — the server refuses it while
  // a turn is in flight, and the composer already owns Stop mid-turn.
  if (meta.state === "idle" || meta.state === "stopped" || meta.state === "failed")
    return "archive";
  return null;
}

interface SessionFinishActionProps {
  meta: SessionMetadata;
  git?: GitActions;
  projectGitStatus?: ProjectGitStatus;
  mainBranch?: string;
  onSendMessage?: (prompt: string) => void;
  onArchive: () => void;
  onUnarchive: () => void;
}

/**
 * The mobile "what's next for this session" slot on the tab strip. It shows the
 * right verb for where the session is — the branch control when the branch
 * needs rebasing, merging or untangling, archive when it does not, a quiet
 * confirmation once merged — and nothing while the session is busy (the
 * composer already owns Stop mid-turn).
 *
 * Desktop puts the same branch control in the header. Same rule, same control,
 * two placements.
 */
export function SessionFinishAction({
  meta,
  git,
  projectGitStatus,
  mainBranch,
  onSendMessage,
  onArchive,
  onUnarchive,
}: SessionFinishActionProps) {
  const kind = finishActionKind(meta, git);

  if (kind === "branch") {
    return (
      <BranchSyncControl
        meta={meta}
        git={git}
        projectGitStatus={projectGitStatus}
        mainBranch={mainBranch}
        onSendMessage={onSendMessage}
        className="h-7 text-xs [&>button]:h-7 [&>button]:text-xs"
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

  if (kind === "archive") {
    const archived = !!meta.archivedAt;
    return (
      <Button
        variant="ghost"
        size="sm"
        className="h-7 shrink-0 gap-1 rounded-md border border-success/40 px-2.5 text-xs text-success hover:bg-success/10"
        title={archived ? "Unarchive session" : "Archive session"}
        onClick={archived ? onUnarchive : onArchive}
      >
        {archived ? <ArchiveRestore className="size-3.5" /> : <Archive className="size-3.5" />}
        {archived ? "Unarchive" : "Archive"}
      </Button>
    );
  }

  return null;
}
