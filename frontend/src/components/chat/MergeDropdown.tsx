import { AlertTriangle, CheckCircle2, GitMerge, Loader2, Trash2 } from "lucide-react";
import { Button } from "~/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "~/components/ui/dropdown-menu";
import type { useGitActions } from "~/hooks/git/useGitActions";

interface MergeDropdownProps {
  git: ReturnType<typeof useGitActions>;
  className?: string;
  projectDirty?: boolean;
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
            <div className="font-medium">Merge & complete</div>
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
            <div className="font-medium">Merge & delete branch</div>
            <div className="text-[11px] text-destructive/60 mt-0.5">
              Merge, remove worktree and branch
            </div>
          </div>
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
