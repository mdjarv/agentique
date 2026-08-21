/**
 * Push/pull pills for a project's main checkout — the one-click remote sync
 * that used to live on the folder-sidebar project row. Placed wherever a
 * project is identifiable: the New-session palette, the session header (the
 * repo you just merged into), and the landing deck.
 *
 * - Push (`↑N`): always mechanical when `aheadRemote > 0`.
 * - Pull (`↓N`):
 *    - Clean FF (no ahead, no uncommitted) → mechanical `--ff-only` pull.
 *    - Messy (diverged or dirty) → open a local session with a rebase prompt.
 */
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
  /** Adds the word "Push"/"Pull" next to the count — for roomier surfaces. */
  labelled?: boolean;
  className?: string;
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

export const ProjectGitPill = memo(function ProjectGitPill({
  projectId,
  projectSlug,
  gitStatus,
  labelled = false,
  className,
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
    <span className={cn("flex shrink-0 items-center gap-0.5", className)}>
      {ahead > 0 && (
        <button
          type="button"
          onClick={onPushClick}
          disabled={pushing}
          title={pushing ? "Pushing..." : `Push ${ahead} commit${ahead === 1 ? "" : "s"}`}
          className="inline-flex cursor-pointer items-center gap-0.5 rounded-full border border-success/30 bg-success/10 px-1.5 py-0.5 text-[10px] font-medium text-success transition-colors hover:bg-success/20 disabled:opacity-50"
        >
          <ArrowUp className="size-2.5" />
          {ahead}
          {labelled && <span className="ml-0.5">{pushing ? "pushing…" : "push"}</span>}
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
            "inline-flex cursor-pointer items-center gap-0.5 rounded-full border px-1.5 py-0.5 text-[10px] font-medium transition-colors disabled:opacity-50",
            pullMessy
              ? "border-warning/40 bg-warning/10 text-warning hover:bg-warning/20"
              : "border-primary/30 bg-primary/10 text-primary hover:bg-primary/20",
          )}
        >
          <ArrowDown className="size-2.5" />
          {behind}
          {labelled && <span className="ml-0.5">{pullMessy ? "rebase" : "pull"}</span>}
        </button>
      )}
    </span>
  );
});
