import { ArrowDown, ArrowUp, GitBranch } from "lucide-react";
import { memo } from "react";
import { useProjectGitActions } from "~/hooks/git/useProjectGitActions";
import type { ProjectGitStatus } from "~/stores/app-store";
import { SidebarRow } from "./SidebarRow";
import { LEVEL } from "./types";

/** Compact git status sub-line shown below the project row when expanded. */
export const ProjectGitLine = memo(function ProjectGitLine({
  projectId,
  gitStatus,
}: {
  projectId: string;
  gitStatus: ProjectGitStatus | undefined;
}) {
  const { pushing, pulling, handlePush, handlePull } = useProjectGitActions(projectId);

  if (!gitStatus?.branch) return null;

  const ahead = gitStatus.aheadRemote > 0;
  const behind = gitStatus.behindRemote > 0;

  return (
    <SidebarRow as="div" indent={LEVEL.project + 1} plain className="!py-0 pb-1 cursor-default">
      <div className="flex items-center gap-1.5 pl-6 w-full">
        {/* Left: branch + counters + dirty */}
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

        {/* Right: push/pull buttons */}
        {(ahead || behind) && (
          <span className="flex items-center gap-1 ml-auto shrink-0">
            {ahead && (
              <button
                type="button"
                onClick={() => handlePush()}
                disabled={pushing}
                className="inline-flex items-center gap-1 text-[10px] font-medium px-1.5 py-0.5 rounded border border-success/30 bg-success/10 text-success hover:bg-success/20 transition-colors disabled:opacity-50 cursor-pointer"
              >
                <ArrowUp className="size-2.5" />
                {pushing ? "..." : "Push"}
              </button>
            )}
            {behind && (
              <button
                type="button"
                onClick={() => handlePull()}
                disabled={pulling}
                className="inline-flex items-center gap-1 text-[10px] font-medium px-1.5 py-0.5 rounded border border-orange/30 bg-orange/10 text-orange hover:bg-orange/20 transition-colors disabled:opacity-50 cursor-pointer"
              >
                <ArrowDown className="size-2.5" />
                {pulling ? "..." : "Pull"}
              </button>
            )}
          </span>
        )}
      </div>
    </SidebarRow>
  );
});
