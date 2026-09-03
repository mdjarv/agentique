import { CheckCircle2 } from "lucide-react";
import { BranchSyncControl } from "~/components/chat/BranchSyncControl";
import type { useGitActions } from "~/hooks/git/useGitActions";
import { hasBranchSync } from "~/lib/session/branch-sync";
import { cn } from "~/lib/utils";
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
  /**
   * The mobile header's metadata line, where everything else is 10-11px. The
   * control keeps its word — the verb is the whole point of it — and gives up
   * the height, which is what lets it ride a line the header already draws
   * instead of a band of its own.
   */
  dense?: boolean;
}

/**
 * The "what's next for this session" slot. It shows the branch control when the
 * branch needs rebasing, merging or untangling, and a quiet confirmation once
 * merged.
 *
 * On mobile it rides the header's metadata line (`dense`), beside the branch
 * facts it acts on; the desktop header carries the same control at full size.
 * Same rule, same control, two sizes.
 */
export function SessionFinishAction({
  meta,
  git,
  projectGitStatus,
  mainBranch,
  onSendMessage,
  dense = false,
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
        className={
          dense
            ? "h-6 text-[10px] [&>button]:h-6 [&>button]:px-2 [&>button]:text-[10px] [&_svg]:size-3"
            : "h-7 text-xs [&>button]:h-7 [&>button]:text-xs"
        }
      />
    );
  }

  if (kind === "merged") {
    return (
      <span
        className={cn(
          "inline-flex shrink-0 items-center gap-1 text-muted-foreground",
          dense ? "h-6 px-1.5 text-[10px]" : "h-7 px-2 text-xs",
        )}
      >
        <CheckCircle2 className={cn("text-success/70", dense ? "size-3" : "size-3.5")} />
        Merged
      </span>
    );
  }

  return null;
}
