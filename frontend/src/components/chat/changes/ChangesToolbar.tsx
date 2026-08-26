import { ChevronDown, ChevronsDownUp, ChevronsUpDown, WrapText } from "lucide-react";
import { Button } from "~/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "~/components/ui/dropdown-menu";
import { cn } from "~/lib/utils";
import { DiffStatBar } from "./types";

/**
 * Which changes the view is showing.
 *
 * - `session` — everything this session has done, measured from the worktree's
 *   base commit. The whole story, committed or not.
 * - `working` — only what is not committed yet, measured from HEAD.
 *
 * Two questions, two answers. Merging them into one list, as this view used to,
 * meant neither could be asked: a file that was committed *and* then edited
 * showed once, under whichever label won.
 */
export type DiffScope = "session" | "working";

interface ChangesToolbarProps {
  scope: DiffScope;
  onScopeChange: (scope: DiffScope) => void;
  /** Offered only when the two scopes can differ — a worktree session. */
  scopeChoice: boolean;
  isWorktree: boolean;
  fileCount: number;
  insertions: number;
  deletions: number;
  allCollapsed: boolean;
  onToggleCollapseAll: () => void;
  wrap: boolean;
  onWrapChange: (wrap: boolean) => void;
}

export function scopeLabel(scope: DiffScope, isWorktree: boolean): string {
  if (scope === "working") return "Uncommitted";
  return isWorktree ? "Branch changes" : "All changes";
}

export function ChangesToolbar({
  scope,
  onScopeChange,
  scopeChoice,
  isWorktree,
  fileCount,
  insertions,
  deletions,
  allCollapsed,
  onToggleCollapseAll,
  wrap,
  onWrapChange,
}: ChangesToolbarProps) {
  const CollapseIcon = allCollapsed ? ChevronsUpDown : ChevronsDownUp;

  return (
    <div className="flex shrink-0 items-center gap-2 border-b px-3 py-1.5 text-xs">
      {scopeChoice ? (
        <DropdownMenu>
          <DropdownMenuTrigger asChild>
            <Button variant="ghost" size="xs" className="-ml-1 gap-1 font-medium">
              {scopeLabel(scope, isWorktree)}
              <ChevronDown className="size-3 opacity-60" />
            </Button>
          </DropdownMenuTrigger>
          <DropdownMenuContent align="start" className="w-56">
            <ScopeItem
              scope="session"
              active={scope === "session"}
              label={scopeLabel("session", isWorktree)}
              hint="Everything since the branch started"
              onSelect={onScopeChange}
            />
            <ScopeItem
              scope="working"
              active={scope === "working"}
              label={scopeLabel("working", isWorktree)}
              hint="Only what is not committed yet"
              onSelect={onScopeChange}
            />
          </DropdownMenuContent>
        </DropdownMenu>
      ) : (
        <span className="font-medium text-foreground">{scopeLabel(scope, isWorktree)}</span>
      )}

      <span className="flex min-w-0 items-center gap-1.5 text-[11px] text-muted-foreground tabular-nums">
        <span>
          {fileCount} {fileCount === 1 ? "file" : "files"}
        </span>
        {insertions > 0 && <span className="text-success">+{insertions}</span>}
        {deletions > 0 && <span className="text-destructive">-{deletions}</span>}
        <DiffStatBar insertions={insertions} deletions={deletions} />
      </span>

      <div className="ml-auto flex shrink-0 items-center">
        <Button
          variant="ghost"
          size="icon-xs"
          onClick={onToggleCollapseAll}
          aria-label={allCollapsed ? "Expand all files" : "Collapse all files"}
          title={allCollapsed ? "Expand all files" : "Collapse all files"}
          className="text-muted-foreground-dim hover:text-foreground"
        >
          <CollapseIcon className="size-3.5" />
        </Button>
        <Button
          variant="ghost"
          size="icon-xs"
          onClick={() => onWrapChange(!wrap)}
          aria-pressed={wrap}
          aria-label={wrap ? "Stop wrapping long lines" : "Wrap long lines"}
          title={wrap ? "Stop wrapping long lines" : "Wrap long lines"}
          className={cn(
            "hover:text-foreground",
            wrap ? "text-primary" : "text-muted-foreground-dim",
          )}
        >
          <WrapText className="size-3.5" />
        </Button>
      </div>
    </div>
  );
}

function ScopeItem({
  scope,
  active,
  label,
  hint,
  onSelect,
}: {
  scope: DiffScope;
  active: boolean;
  label: string;
  hint: string;
  onSelect: (scope: DiffScope) => void;
}) {
  return (
    <DropdownMenuItem
      className={cn("flex-col items-start gap-0.5 py-2 text-xs", active && "bg-accent")}
      onClick={() => onSelect(scope)}
    >
      <span className="font-medium">{label}</span>
      <span className="text-[11px] text-muted-foreground">{hint}</span>
    </DropdownMenuItem>
  );
}
