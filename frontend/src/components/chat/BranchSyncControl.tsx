import { AlertTriangle, ArrowDown, ChevronDown, Loader2, RefreshCw, Sparkles } from "lucide-react";
import { MergeDropdown, MergeMenuItems } from "~/components/chat/MergeDropdown";
import { Button } from "~/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuTrigger,
} from "~/components/ui/dropdown-menu";
import type { useGitActions } from "~/hooks/git/useGitActions";
import { branchSync, resolveConflictsPrompt } from "~/lib/session/branch-sync";
import { cn } from "~/lib/utils";
import type { ProjectGitStatus } from "~/stores/app-store";
import type { SessionMetadata } from "~/stores/chat-store";

type GitActions = ReturnType<typeof useGitActions>;

/**
 * One slot, naming the verb the branch actually needs.
 *
 * Colour says what a click on the body does, and it says the same thing on both
 * layouts: **orange acts, green opens a menu.** Rebase runs on click because it
 * is the common move and the one that should be cheap; Merge opens its three
 * modes, as it always has.
 *
 * On a diverged branch the control is a split: Rebase on the body, and the
 * merge menu behind the caret. That demotion is the point rather than a
 * compromise — a `--ff-only` merge of a branch that is behind cannot
 * fast-forward, so merging first only ever ends in the server asking for a
 * rebase. Making rebase the one-click path is that answer, one step earlier.
 *
 * When the merge-tree dry run says the branch will not apply, neither verb is
 * offered: the control becomes "Resolve conflicts", which hands the named files
 * to the agent — the same move, and the same prompt, the Changes tab has always
 * offered.
 */
export function BranchSyncControl({
  meta,
  git,
  projectGitStatus,
  mainBranch,
  onSendMessage,
  className,
}: {
  meta: SessionMetadata;
  git?: GitActions;
  projectGitStatus?: ProjectGitStatus;
  mainBranch?: string;
  /** Sends the conflict hand-off to the agent. Without it that state renders nothing. */
  onSendMessage?: (prompt: string) => void;
  className?: string;
}) {
  const sync = branchSync(meta, !!git);
  if (sync.kind === "none" || !git) return null;

  const projectDirty = !!projectGitStatus && projectGitStatus.uncommittedCount > 0;
  const hasUncommitted = !!git.uncommittedFiles && git.uncommittedFiles.length > 0;

  if (sync.kind === "conflicts") {
    if (!onSendMessage) return null;
    return (
      <Button
        variant="ghost"
        size="sm"
        className={cn(
          "gap-1 border border-warning/40 bg-warning/10 text-warning hover:bg-warning/20",
          className,
        )}
        title={
          sync.files.length > 0
            ? `Conflicts in ${sync.files.join(", ")} — Claude will rebase and resolve them`
            : "Claude will rebase onto main and resolve the conflicts"
        }
        onClick={() => onSendMessage(resolveConflictsPrompt(sync.files))}
      >
        <AlertTriangle className="h-3 w-3" />
        Resolve
        <Sparkles className="h-2.5 w-2.5 text-primary/70" />
      </Button>
    );
  }

  if (sync.kind === "merge") {
    return (
      <MergeDropdown
        git={git}
        projectDirty={projectDirty}
        className={cn(
          "border",
          meta.mergeStatus === "clean" && !hasUncommitted
            ? "bg-success/10 text-success border-success/30 hover:bg-success/20"
            : "hover:bg-accent",
          className,
        )}
      />
    );
  }

  // Rebase leads. `autoCommit` runs on the server before the replay, which is
  // not visible anywhere else in the UI — so the tooltip is where it gets said.
  const rebaseTitle = `Rebase onto ${mainBranch || "main"} — ${sync.behind} ${
    sync.behind === 1 ? "commit" : "commits"
  } behind. Commits any pending changes first.`;

  const rebaseBody = (
    <>
      {git.rebasing ? (
        <Loader2 className="h-3 w-3 animate-spin" />
      ) : (
        <RefreshCw className="h-3 w-3" />
      )}
      Rebase
      <span className="inline-flex items-center tabular-nums opacity-90">
        <ArrowDown className="h-2.5 w-2.5" />
        {sync.behind}
      </span>
    </>
  );

  const rebaseTone = "border-orange/40 bg-orange/10 text-orange hover:bg-orange/20";

  if (sync.kind === "rebase") {
    return (
      <Button
        variant="ghost"
        size="sm"
        className={cn("gap-1 border", rebaseTone, className)}
        title={rebaseTitle}
        onClick={git.handleRebase}
        disabled={git.rebasing}
      >
        {rebaseBody}
      </Button>
    );
  }

  // Diverged: one control, two hit targets. They share a border so it reads as
  // one thing, and the caret keeps its own generous tap area — the merge menu
  // has to stay reachable with a thumb.
  return (
    <div className={cn("inline-flex items-stretch", className)}>
      <Button
        variant="ghost"
        size="sm"
        className={cn("gap-1 rounded-r-none border border-r-0 pr-2", rebaseTone)}
        title={rebaseTitle}
        onClick={git.handleRebase}
        disabled={git.rebasing}
      >
        {rebaseBody}
      </Button>
      <DropdownMenu>
        <DropdownMenuTrigger asChild>
          <Button
            variant="ghost"
            size="sm"
            className={cn("rounded-l-none border px-1.5", rebaseTone)}
            title="Merge instead"
            aria-label="Merge instead"
            disabled={git.merging}
          >
            {git.merging ? (
              <Loader2 className="h-3 w-3 animate-spin" />
            ) : (
              <ChevronDown className="h-3 w-3" />
            )}
            {projectDirty && <AlertTriangle className="h-2.5 w-2.5 text-warning" />}
          </Button>
        </DropdownMenuTrigger>
        <DropdownMenuContent align="end" className="w-60">
          <MergeMenuItems git={git} behind={sync.behind} mainBranch={mainBranch} />
        </DropdownMenuContent>
      </DropdownMenu>
    </div>
  );
}
