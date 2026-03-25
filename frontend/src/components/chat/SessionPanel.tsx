import {
  AlertTriangle,
  ArrowDown,
  ArrowRight,
  ArrowUp,
  CheckCircle2,
  ExternalLink,
  FileDiff,
  GitBranch,
  GitCommitHorizontal,
  GitMerge,
  ListTodo,
  Loader2,
  Minus,
  PanelRightClose,
  RefreshCw,
} from "lucide-react";
import { Button } from "~/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "~/components/ui/dropdown-menu";
import type { useGitActions } from "~/hooks/useGitActions";
import { cn } from "~/lib/utils";
import type { SessionMetadata, TodoItem } from "~/stores/chat-store";
import { TodoItemRow } from "./TodoPanel";

interface SessionPanelProps {
  meta: SessionMetadata;
  todos: TodoItem[] | null;
  git: ReturnType<typeof useGitActions>;
  onCollapse: () => void;
  onSendMessage?: (prompt: string) => void;
  onOpenDialog?: (dialog: "pr" | "commit") => void;
}

function MergeStatusBanner({
  meta,
  git,
  onSendMessage,
}: {
  meta: SessionMetadata;
  git: ReturnType<typeof useGitActions>;
  onSendMessage?: (prompt: string) => void;
}) {
  const isBusy = meta.state === "running";

  if (meta.worktreeMerged) {
    return (
      <div className="rounded-md border border-[#9ece6a]/20 bg-[#9ece6a]/5 px-3 py-2">
        <div className="flex items-center gap-2 text-xs text-[#9ece6a]/80">
          <CheckCircle2 className="h-3.5 w-3.5" />
          Merged
        </div>
      </div>
    );
  }

  if (meta.mergeStatus === "conflicts") {
    return (
      <div className="rounded-md border border-amber-500/20 bg-amber-500/5 px-3 py-2 space-y-2">
        <div className="flex items-center gap-2 text-xs font-medium text-amber-500">
          <AlertTriangle className="h-3.5 w-3.5" />
          {meta.mergeConflictFiles?.length ?? 0} file(s) have conflicts
        </div>
        {meta.mergeConflictFiles && meta.mergeConflictFiles.length > 0 && (
          <ul className="text-[11px] text-amber-400/70 space-y-0.5 pl-5.5">
            {meta.mergeConflictFiles.map((f) => (
              <li key={f} className="font-mono truncate">
                {f}
              </li>
            ))}
          </ul>
        )}
        {!isBusy && (
          <Button
            variant="ghost"
            size="sm"
            className="h-6 px-2 text-xs text-amber-500 hover:text-amber-400 hover:bg-amber-500/10"
            onClick={() => {
              const files = meta.mergeConflictFiles?.join(", ") ?? "";
              onSendMessage?.(
                `Rebase your branch onto main and resolve any merge conflicts. The following files have conflicts: ${files}. After resolving, stage each file and continue the rebase with \`git rebase --continue\`.`,
              );
            }}
          >
            Rebase & Resolve
          </Button>
        )}
      </div>
    );
  }

  const hasAhead = (meta.commitsAhead ?? 0) > 0;
  const hasBehind = (meta.commitsBehind ?? 0) > 0;

  if (meta.mergeStatus === "clean" && hasAhead) {
    return (
      <div className="rounded-md border border-[#9ece6a]/20 bg-[#9ece6a]/5 px-3 py-2 space-y-2">
        <div className="flex items-center gap-2 text-xs font-medium text-[#9ece6a]">
          <CheckCircle2 className="h-3.5 w-3.5" />
          Ready to merge
        </div>
        {!isBusy && (
          <DropdownMenu>
            <DropdownMenuTrigger asChild>
              <Button
                variant="ghost"
                size="sm"
                className="h-6 px-2 text-xs text-[#9ece6a] hover:bg-[#9ece6a]/10"
                disabled={git.merging}
              >
                {git.merging ? (
                  <Loader2 className="h-3 w-3 animate-spin" />
                ) : (
                  <GitMerge className="h-3 w-3" />
                )}
                Merge
              </Button>
            </DropdownMenuTrigger>
            <DropdownMenuContent align="start">
              <DropdownMenuItem onClick={() => git.handleMerge(false)} className="text-xs">
                Merge
              </DropdownMenuItem>
              <DropdownMenuItem onClick={() => git.handleMerge(true)} className="text-xs">
                Merge & clean up
              </DropdownMenuItem>
            </DropdownMenuContent>
          </DropdownMenu>
        )}
      </div>
    );
  }

  if (hasBehind) {
    return (
      <div className="rounded-md border border-[#7aa2f7]/20 bg-[#7aa2f7]/5 px-3 py-2 space-y-2">
        <div className="flex items-center gap-2 text-xs text-[#7aa2f7]">
          <ArrowDown className="h-3.5 w-3.5" />
          Behind by {meta.commitsBehind} commit(s)
        </div>
        {!isBusy && (
          <Button
            variant="ghost"
            size="sm"
            className="h-6 px-2 text-xs text-[#7aa2f7] hover:bg-[#7aa2f7]/10"
            onClick={git.handleRebase}
            disabled={git.rebasing}
          >
            {git.rebasing ? <Loader2 className="h-3 w-3 animate-spin" /> : null}
            Rebase
          </Button>
        )}
      </div>
    );
  }

  if (hasAhead) {
    return (
      <div className="rounded-md border border-border/50 bg-muted/30 px-3 py-2 space-y-2">
        <div className="flex items-center gap-2 text-xs text-muted-foreground">
          <Minus className="h-3.5 w-3.5" />
          Up to date
        </div>
        {!isBusy && (
          <DropdownMenu>
            <DropdownMenuTrigger asChild>
              <Button variant="ghost" size="sm" className="h-6 px-2 text-xs" disabled={git.merging}>
                {git.merging ? (
                  <Loader2 className="h-3 w-3 animate-spin" />
                ) : (
                  <GitMerge className="h-3 w-3" />
                )}
                Merge
              </Button>
            </DropdownMenuTrigger>
            <DropdownMenuContent align="start">
              <DropdownMenuItem onClick={() => git.handleMerge(false)} className="text-xs">
                Merge
              </DropdownMenuItem>
              <DropdownMenuItem onClick={() => git.handleMerge(true)} className="text-xs">
                Merge & clean up
              </DropdownMenuItem>
            </DropdownMenuContent>
          </DropdownMenu>
        )}
      </div>
    );
  }

  return null;
}

function GitSection({
  meta,
  git,
  onSendMessage,
  onOpenDialog,
}: {
  meta: SessionMetadata;
  git: ReturnType<typeof useGitActions>;
  onSendMessage?: (prompt: string) => void;
  onOpenDialog?: (dialog: "pr" | "commit") => void;
}) {
  const isWorktree = !!meta.worktreeBranch;
  const isBusy = meta.state === "running";

  return (
    <div className="space-y-3">
      {/* Section header with refresh */}
      <div className="flex items-center justify-between">
        <span className="text-xs font-medium text-muted-foreground uppercase tracking-wider">
          Git
        </span>
        {isWorktree && !isBusy && (
          <button
            type="button"
            onClick={git.handleRefreshGit}
            disabled={git.refreshingGit}
            className="p-0.5 rounded hover:bg-muted text-muted-foreground hover:text-foreground transition-colors"
          >
            <RefreshCw className={cn("h-3 w-3", git.refreshingGit && "animate-spin")} />
          </button>
        )}
      </div>

      {/* Branch line */}
      {isWorktree && (
        <div className="flex items-center gap-1.5 text-xs text-muted-foreground">
          <GitBranch className="h-3 w-3 shrink-0" />
          <span className="font-mono truncate">{meta.worktreeBranch}</span>
          <ArrowRight className="h-3 w-3 shrink-0 text-muted-foreground/50" />
          <span className="font-mono">master</span>
        </div>
      )}

      {/* Commits ahead/behind */}
      {isWorktree && !meta.worktreeMerged && !meta.branchMissing && (
        <div className="flex items-center gap-3 text-xs">
          {(meta.commitsAhead ?? 0) > 0 && (
            <span className="flex items-center gap-1 text-muted-foreground">
              <ArrowUp className="h-3 w-3" />
              {meta.commitsAhead} ahead
            </span>
          )}
          {(meta.commitsBehind ?? 0) > 0 && (
            <span className="flex items-center gap-1 text-[#7aa2f7]/80">
              <ArrowDown className="h-3 w-3" />
              {meta.commitsBehind} behind
            </span>
          )}
        </div>
      )}

      {/* Merge status banner */}
      {isWorktree && !meta.branchMissing && (
        <MergeStatusBanner meta={meta} git={git} onSendMessage={onSendMessage} />
      )}

      {meta.branchMissing && <div className="text-xs text-[#f7768e]/80">Branch missing</div>}

      {/* Diff summary */}
      {git.diffTotals && (git.diffTotals.add > 0 || git.diffTotals.del > 0) && (
        <button
          type="button"
          onClick={git.toggleDiff}
          className={cn(
            "flex items-center gap-1.5 text-xs transition-colors w-full text-left",
            git.showDiff ? "text-foreground" : "text-muted-foreground hover:text-foreground",
          )}
        >
          <FileDiff className="h-3 w-3 shrink-0" />
          <span className="text-green-500">+{git.diffTotals.add}</span>
          <span className="text-red-500">-{git.diffTotals.del}</span>
          <span>across {git.diffResult?.files.length ?? 0} files</span>
        </button>
      )}

      {/* PR */}
      {meta.prUrl ? (
        <a
          href={meta.prUrl}
          target="_blank"
          rel="noreferrer"
          className="flex items-center gap-1.5 text-xs text-[#7aa2f7]/80 hover:text-[#7aa2f7] transition-colors"
        >
          <ExternalLink className="h-3 w-3" />
          Pull Request
        </a>
      ) : isWorktree && !isBusy ? (
        <Button
          variant="ghost"
          size="sm"
          className="h-6 px-2 text-xs w-full justify-start"
          onClick={() => onOpenDialog?.("pr")}
          disabled={git.creatingPR}
        >
          {git.creatingPR ? (
            <Loader2 className="h-3 w-3 animate-spin" />
          ) : (
            <ExternalLink className="h-3 w-3" />
          )}
          Create PR
        </Button>
      ) : null}

      {/* Commit for non-worktree sessions */}
      {!isWorktree && !isBusy && (
        <Button
          variant="ghost"
          size="sm"
          className="h-6 px-2 text-xs w-full justify-start"
          onClick={() => onOpenDialog?.("commit")}
          disabled={git.committing}
        >
          {git.committing ? (
            <Loader2 className="h-3 w-3 animate-spin" />
          ) : (
            <GitCommitHorizontal className="h-3 w-3" />
          )}
          Commit
        </Button>
      )}
    </div>
  );
}

function TodoSection({ todos }: { todos: TodoItem[] }) {
  const completed = todos.filter((t) => t.status === "completed").length;

  return (
    <div className="space-y-2">
      <div className="flex items-center gap-2">
        <span className="text-xs font-medium text-muted-foreground uppercase tracking-wider">
          Todos
        </span>
        <span className="text-xs text-muted-foreground/60">
          {completed}/{todos.length}
        </span>
      </div>
      <div className="space-y-0">
        {todos.map((item, i) => (
          <TodoItemRow key={`${i}-${item.content}`} item={item} />
        ))}
      </div>
    </div>
  );
}

export function SessionPanel({
  meta,
  todos,
  git,
  onCollapse,
  onSendMessage,
  onOpenDialog,
}: SessionPanelProps) {
  const isWorktree = !!meta.worktreeBranch;
  const hasTodos = todos !== null && todos.length > 0;

  return (
    <div className="w-72 border-l flex flex-col h-full bg-background shrink-0">
      {/* Header */}
      <div className="shrink-0 px-3 py-2 border-b flex items-center gap-2">
        <span className="text-xs font-medium text-muted-foreground">Session</span>
        <button
          type="button"
          onClick={onCollapse}
          className="ml-auto p-0.5 rounded hover:bg-muted text-muted-foreground hover:text-foreground transition-colors"
        >
          <PanelRightClose className="h-3.5 w-3.5" />
        </button>
      </div>

      {/* Scrollable content */}
      <div className="flex-1 overflow-y-auto px-3 py-3 space-y-4">
        {(isWorktree || !hasTodos) && (
          <GitSection
            meta={meta}
            git={git}
            onSendMessage={onSendMessage}
            onOpenDialog={onOpenDialog}
          />
        )}

        {hasTodos && (
          <>
            {isWorktree && <div className="border-t" />}
            <TodoSection todos={todos} />
          </>
        )}
      </div>
    </div>
  );
}

interface CollapsedSessionStripProps {
  meta: SessionMetadata;
  todos: TodoItem[] | null;
  onExpand: () => void;
}

export function CollapsedSessionStrip({ meta, todos, onExpand }: CollapsedSessionStripProps) {
  const hasTodos = todos !== null && todos.length > 0;
  const completed = hasTodos ? todos.filter((t) => t.status === "completed").length : 0;

  let mergeIcon = null;
  if (meta.worktreeMerged) {
    mergeIcon = <CheckCircle2 className="h-3.5 w-3.5 text-[#9ece6a]/70" />;
  } else if (meta.mergeStatus === "conflicts") {
    mergeIcon = <AlertTriangle className="h-3.5 w-3.5 text-amber-500/70" />;
  } else if (meta.mergeStatus === "clean" && (meta.commitsAhead ?? 0) > 0) {
    mergeIcon = <CheckCircle2 className="h-3.5 w-3.5 text-[#9ece6a]/50" />;
  } else if (meta.worktreeBranch) {
    mergeIcon = <GitBranch className="h-3.5 w-3.5 text-muted-foreground/50" />;
  }

  return (
    <button
      type="button"
      onClick={onExpand}
      className="w-9 border-l flex flex-col items-center py-3 gap-3 shrink-0 hover:bg-muted/50 transition-colors"
    >
      {mergeIcon}
      {hasTodos && (
        <>
          <ListTodo className="h-3.5 w-3.5 text-muted-foreground/50" />
          <span className="text-[10px] text-muted-foreground/60">
            {completed}/{todos.length}
          </span>
        </>
      )}
    </button>
  );
}
