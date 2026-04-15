import { Link } from "@tanstack/react-router";
import { GitCommitHorizontal, Settings, X } from "lucide-react";
import { memo, useCallback, useMemo } from "react";
import { useShallow } from "zustand/shallow";
import { Button } from "~/components/ui/button";
import { useProjectGitActions } from "~/hooks/git/useProjectGitActions";
import { useProjectIcon } from "~/hooks/useProjectIcon";
import { useTheme } from "~/hooks/useTheme";
import { getProjectColor } from "~/lib/project-colors";
import type { Project } from "~/lib/types";
import { type ProjectGitStatus, useAppStore } from "~/stores/app-store";
import { BranchSelector } from "../git/BranchSelector";
import { CommitPopover } from "../git/CommitPopover";

interface ProjectContextBarProps {
  project: Project;
  onClose: () => void;
}

/** Shown when a project is selected in the rail/strip. Two zones: identity (name) and git (branch + actions). */
export const ProjectContextBar = memo(function ProjectContextBar({
  project,
  onClose,
}: ProjectContextBarProps) {
  const { resolvedTheme } = useTheme();
  const Icon = useProjectIcon(project.icon);
  const projectIds = useAppStore(useShallow((s) => s.projects.map((p) => p.id)));
  const color = useMemo(
    () => getProjectColor(project.color, project.id, projectIds, resolvedTheme),
    [project.color, project.id, projectIds, resolvedTheme],
  );
  const initials = project.slug
    .split("-")
    .map((w) => w[0])
    .join("")
    .toUpperCase()
    .slice(0, 2);

  const gitStatus = useAppStore(
    (s): ProjectGitStatus | undefined => s.projectGitStatus[project.id],
  );

  const {
    pushing,
    pulling,
    fetching,
    committing,
    handlePush,
    handlePull,
    handleFetch,
    handleCommit,
  } = useProjectGitActions(project.id);

  const handleBranchChanged = useCallback((status: ProjectGitStatus) => {
    useAppStore.getState().setProjectGitStatus(status);
  }, []);

  const ahead = gitStatus && gitStatus.aheadRemote > 0;
  const behind = gitStatus && gitStatus.behindRemote > 0;
  const canPush = ahead && gitStatus?.hasRemote;
  const canPull = behind && gitStatus?.hasRemote;
  const canFetch = gitStatus?.hasRemote;

  return (
    <div className="border-b border-border/50" style={{ borderTop: `2px solid ${color.bg}40` }}>
      {/* Identity zone: project name */}
      <div className="flex items-center gap-2 px-3 pt-2 pb-2">
        <span
          className="size-6 rounded-md flex items-center justify-center text-[10px] font-bold shrink-0"
          style={{ backgroundColor: `${color.bg}20`, color: color.fg }}
        >
          {Icon ? <Icon className="size-3.5" /> : initials}
        </span>
        <span className="text-sm font-semibold flex-1 min-w-0 truncate" style={{ color: color.fg }}>
          {project.name}
        </span>
        <Link
          to="/project/$projectSlug/settings"
          params={{ projectSlug: project.slug }}
          className="size-6 flex items-center justify-center rounded-md text-muted-foreground-faint hover:text-foreground transition-colors"
        >
          <Settings className="size-3.5" />
        </Link>
        <Button variant="ghost" size="icon-sm" onClick={onClose} className="size-6">
          <X className="size-3.5" />
        </Button>
      </div>

      {/* Git zone: branch + contextual actions */}
      {gitStatus?.branch && (
        <>
          <div className="h-px bg-border/50 mx-3" />
          <div className="flex items-center justify-between gap-2 px-3 py-2 flex-wrap">
            <div className="flex items-center gap-2 min-w-0">
              <BranchSelector
                projectId={project.id}
                currentBranch={gitStatus.branch}
                isDirty={gitStatus.uncommittedCount > 0}
                onBranchChanged={handleBranchChanged}
              />
              {gitStatus.uncommittedCount > 0 && (
                <>
                  <Link
                    to="/project/$projectSlug"
                    params={{ projectSlug: project.slug }}
                    className="text-[10px] text-warning/80 hover:text-warning hover:underline transition-colors"
                  >
                    {gitStatus.uncommittedCount} dirty
                  </Link>
                  <CommitPopover onCommit={handleCommit} committing={committing}>
                    <button
                      type="button"
                      className="inline-flex items-center justify-center size-5 rounded-md text-warning/80 hover:text-warning hover:bg-warning/15 transition-colors cursor-pointer"
                      title="Commit all changes"
                    >
                      <GitCommitHorizontal className="size-3" />
                    </button>
                  </CommitPopover>
                </>
              )}
            </div>
            {(canPush || canPull || canFetch) && (
              <div className="flex items-center gap-1.5">
                {canPush && (
                  <button
                    type="button"
                    onClick={handlePush}
                    disabled={pushing}
                    className="inline-flex items-center gap-1 text-[11px] font-medium px-2 py-0.5 rounded-md bg-success/15 text-success hover:bg-success/25 transition-colors disabled:opacity-50 cursor-pointer"
                  >
                    {pushing ? "Pushing\u2026" : `\u2191 Push ${gitStatus.aheadRemote}`}
                  </button>
                )}
                {canPull && (
                  <button
                    type="button"
                    onClick={handlePull}
                    disabled={pulling}
                    className="inline-flex items-center gap-1 text-[11px] font-medium px-2 py-0.5 rounded-md bg-orange/15 text-orange hover:bg-orange/25 transition-colors disabled:opacity-50 cursor-pointer"
                  >
                    {pulling ? "Pulling\u2026" : `\u2193 Pull ${gitStatus.behindRemote}`}
                  </button>
                )}
                {canFetch && (
                  <button
                    type="button"
                    onClick={handleFetch}
                    disabled={fetching}
                    className="text-[11px] font-medium text-muted-foreground hover:text-foreground transition-colors disabled:opacity-50 cursor-pointer"
                  >
                    {fetching ? "Fetching\u2026" : "Fetch"}
                  </button>
                )}
              </div>
            )}
          </div>
        </>
      )}
    </div>
  );
});
