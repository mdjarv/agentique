import { useNavigate } from "@tanstack/react-router";
import { ArrowDown, ArrowUp } from "lucide-react";
import { memo, useCallback } from "react";
import { useProjectGitActions } from "~/hooks/git/useProjectGitActions";
import { cn } from "~/lib/utils";
import type { ProjectGitStatus } from "~/stores/app-store";
import { useAppStore } from "~/stores/app-store";

interface ProjectGitPillProps {
  projectId: string;
  projectSlug: string;
  gitStatus: ProjectGitStatus | undefined;
}

/** Rebase prompt used when pull is non-FF (diverged or dirty while behind). */
function buildRebasePrompt(s: ProjectGitStatus): string {
  const branch = s.branch || "the current branch";
  return (
    `Project is behind \`origin/${branch}\` by ${s.behindRemote} commits ` +
    `and ahead by ${s.aheadRemote}. Pull is non-FF. ` +
    `Please rebase local commits onto upstream, resolve any conflicts, ` +
    `and verify tests pass before pushing.`
  );
}

/**
 * Inline push/pull pills for the project row.
 *
 * - Push (`↑N`): always mechanical when `aheadRemote > 0`.
 * - Pull (`↓N`):
 *    - Clean FF (no ahead, no uncommitted) → mechanical `--ff-only` pull.
 *    - Messy (diverged or dirty) → open new Local session with a rebase prompt.
 */
export const ProjectGitPill = memo(function ProjectGitPill({
  projectId,
  projectSlug,
  gitStatus,
}: ProjectGitPillProps) {
  const navigate = useNavigate();
  const { pushing, pulling, handlePush, handlePull } = useProjectGitActions(projectId);

  const ahead = gitStatus?.aheadRemote ?? 0;
  const behind = gitStatus?.behindRemote ?? 0;
  const uncommitted = gitStatus?.uncommittedCount ?? 0;

  const onPushClick = useCallback(
    (e: React.MouseEvent) => {
      e.stopPropagation();
      handlePush();
    },
    [handlePush],
  );

  const onPullClick = useCallback(
    (e: React.MouseEvent) => {
      e.stopPropagation();
      const status = useAppStore.getState().projectGitStatus[projectId];
      if (!status) return;
      const messy = status.aheadRemote > 0 || status.uncommittedCount > 0;
      if (!messy) {
        handlePull();
        return;
      }
      useAppStore.getState().setSidebarOpen(false);
      navigate({
        to: "/project/$projectSlug/session/new",
        params: { projectSlug },
        search: { prompt: buildRebasePrompt(status), worktree: false },
      });
    },
    [handlePull, navigate, projectId, projectSlug],
  );

  if (!gitStatus?.hasRemote) return null;
  if (ahead === 0 && behind === 0) return null;

  const pullMessy = behind > 0 && (ahead > 0 || uncommitted > 0);

  return (
    <span className="flex items-center gap-0.5 shrink-0">
      {ahead > 0 && (
        <button
          type="button"
          onClick={onPushClick}
          disabled={pushing}
          title={pushing ? "Pushing..." : `Push ${ahead} commit${ahead === 1 ? "" : "s"}`}
          className="inline-flex items-center gap-0.5 text-[10px] font-medium px-1.5 py-0.5 rounded-full border border-success/30 bg-success/10 text-success hover:bg-success/20 transition-colors disabled:opacity-50 cursor-pointer"
        >
          <ArrowUp className="size-2.5" />
          {ahead}
        </button>
      )}
      {behind > 0 && (
        <button
          type="button"
          onClick={onPullClick}
          disabled={pulling}
          title={
            pulling
              ? "Pulling..."
              : pullMessy
                ? `${behind} behind — non-FF, opens rebase session`
                : `Pull ${behind} commit${behind === 1 ? "" : "s"} (fast-forward)`
          }
          className={cn(
            "inline-flex items-center gap-0.5 text-[10px] font-medium px-1.5 py-0.5 rounded-full border transition-colors disabled:opacity-50 cursor-pointer",
            pullMessy
              ? "border-warning/40 bg-warning/10 text-warning hover:bg-warning/20"
              : "border-primary/30 bg-primary/10 text-primary hover:bg-primary/20",
          )}
        >
          <ArrowDown className="size-2.5" />
          {behind}
        </button>
      )}
    </span>
  );
});
