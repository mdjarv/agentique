import { AlertTriangle, CheckCircle2, GitMerge, Loader2, Trash2 } from "lucide-react";
import { Button } from "~/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "~/components/ui/dropdown-menu";
import type { useGitActions } from "~/hooks/git/useGitActions";

interface MergeDropdownProps {
  git: ReturnType<typeof useGitActions>;
  className?: string;
  projectDirty?: boolean;
}

/**
 * The three merge modes, declared once.
 *
 * Split from the trigger because two controls open this same menu: the plain
 * Merge dropdown, and the caret on the rebase-first control a diverged branch
 * gets. A second copy would be a second place to forget a mode.
 *
 * `behind` is set only on a diverged branch. It renders a label rather than
 * disabling anything: merging while behind is a legitimate call, it is just not
 * the one that will succeed first — the server's `--ff-only` merge answers
 * `needs_rebase` and the client offers "Rebase & Merge" from there. Saying so
 * up front means the toast is a confirmation rather than a surprise.
 */
export function MergeMenuItems({
  git,
  behind = 0,
  mainBranch,
}: {
  git: ReturnType<typeof useGitActions>;
  behind?: number;
  mainBranch?: string;
}) {
  return (
    <>
      {behind > 0 && (
        <>
          <DropdownMenuLabel className="text-[11px] font-normal text-muted-foreground">
            Behind {mainBranch || "main"} by {behind} — merging will rebase first
          </DropdownMenuLabel>
          <DropdownMenuSeparator />
        </>
      )}
      <DropdownMenuItem onClick={() => git.handleMerge("merge")} className="text-xs gap-2.5 py-2">
        <GitMerge className="h-3.5 w-3.5 text-muted-foreground-dim" />
        <div>
          <div className="font-medium">Merge</div>
          <div className="text-[11px] text-muted-foreground mt-0.5">
            Merge into main, keep session
          </div>
        </div>
      </DropdownMenuItem>
      <DropdownMenuItem
        onClick={() => git.handleMerge("complete")}
        className="text-xs gap-2.5 py-2"
      >
        <CheckCircle2 className="h-3.5 w-3.5 text-success/70" />
        <div>
          <div className="font-medium">Merge &amp; complete</div>
          <div className="text-[11px] text-muted-foreground mt-0.5">
            Merge and mark session done
          </div>
        </div>
      </DropdownMenuItem>
      <DropdownMenuSeparator />
      <DropdownMenuItem
        onClick={() => git.handleMerge("delete")}
        className="text-xs gap-2.5 py-2 text-destructive focus:text-destructive"
      >
        <Trash2 className="h-3.5 w-3.5" />
        <div>
          <div className="font-medium">Merge &amp; delete branch</div>
          <div className="text-[11px] text-destructive/60 mt-0.5">
            Merge, remove worktree and branch
          </div>
        </div>
      </DropdownMenuItem>
    </>
  );
}

export function MergeDropdown({ git, projectDirty, className }: MergeDropdownProps) {
  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button variant="ghost" size="sm" className={className} disabled={git.merging}>
          {git.merging ? (
            <Loader2 className="h-3 w-3 animate-spin" />
          ) : (
            <GitMerge className="h-3 w-3" />
          )}
          Merge
          {projectDirty && <AlertTriangle className="h-2.5 w-2.5 text-warning" />}
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="start" className="w-56">
        <MergeMenuItems git={git} />
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
