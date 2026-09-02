import { CheckCircle2 } from "lucide-react";
import { BranchSyncControl } from "~/components/chat/BranchSyncControl";
import type { useGitActions } from "~/hooks/git/useGitActions";
import { hasBranchSync } from "~/lib/session/branch-sync";
import type { ProjectGitStatus } from "~/stores/app-store";
import type { SessionMetadata } from "~/stores/chat-store";

type GitActions = ReturnType<typeof useGitActions>;

export type FinishActionKind = "branch" | "merged" | null;

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
  return null;
}

interface SessionFinishActionProps {
  meta: SessionMetadata;
  git?: GitActions;
  projectGitStatus?: ProjectGitStatus;
  mainBranch?: string;
  onSendMessage?: (prompt: string) => void;
}

/**
 * The mobile "what's next for this session" slot on the tab strip. It shows the
 * branch control when the branch needs rebasing, merging or untangling, or a
 * quiet confirmation once merged. Pin and archive stay in the header.
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

  return null;
}
