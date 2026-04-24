import { GitBranch } from "lucide-react";
import { memo } from "react";
import type { ProjectGitStatus } from "~/stores/app-store";
import { SidebarRow } from "./SidebarRow";
import { LEVEL } from "./types";

/**
 * Compact git status sub-line shown below the project row when expanded.
 * Informational only — push/pull is on the project row itself via `ProjectGitPill`.
 */
export const ProjectGitLine = memo(function ProjectGitLine({
  gitStatus,
}: {
  gitStatus: ProjectGitStatus | undefined;
}) {
  if (!gitStatus?.branch) return null;

  const ahead = gitStatus.aheadRemote > 0;
  const behind = gitStatus.behindRemote > 0;

  return (
    <SidebarRow as="div" indent={LEVEL.project + 1} plain className="!py-0 pb-1 cursor-default">
      <div className="flex items-center gap-1.5 pl-6 w-full">
        <span className="inline-flex items-center gap-1 text-[11px] text-muted-foreground truncate">
          <GitBranch className="size-3 shrink-0" />
          <span className="truncate">{gitStatus.branch}</span>
          {ahead && <span className="text-success shrink-0">&uarr;{gitStatus.aheadRemote}</span>}
          {behind && <span className="text-orange shrink-0">&darr;{gitStatus.behindRemote}</span>}
        </span>
        {gitStatus.uncommittedCount > 0 && (
          <span className="text-[10px] text-warning font-medium">
            {gitStatus.uncommittedCount} dirty
          </span>
        )}
      </div>
    </SidebarRow>
  );
});
