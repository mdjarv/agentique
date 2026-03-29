import {
  AlertTriangle,
  ArrowDown,
  ArrowRight,
  ArrowUp,
  CheckCircle2,
  ChevronDown,
  ChevronRight,
  ExternalLink,
  FileMinus,
  FilePlus,
  FileQuestion,
  FileText,
  GitBranch,
  GitCommitHorizontal,
  GitMerge,
  ListTodo,
  Loader2,
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

// --- Merge dropdown (shared between ready-to-merge and has-ahead states) ---

function MergeDropdown({
  git,
  className,
}: {
  git: ReturnType<typeof useGitActions>;
  className?: string;
}) {
  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button
          variant="ghost"
          size="sm"
          className={cn("h-6 px-2 text-xs", className)}
          disabled={git.merging}
        >
          {git.merging ? (
            <Loader2 className="h-3.5 w-3.5 animate-spin" />
          ) : (
            <GitMerge className="h-3.5 w-3.5" />
          )}
          Merge
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="start">
        <DropdownMenuItem onClick={() => git.handleMerge("merge")} className="text-xs">
          Merge
        </DropdownMenuItem>
        <DropdownMenuItem onClick={() => git.handleMerge("complete")} className="text-xs">
          Merge & complete
        </DropdownMenuItem>
        <DropdownMenuItem onClick={() => git.handleMerge("delete")} className="text-xs">
          Merge & delete branch
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}

// --- Branch status: ahead/behind + merge readiness + actions ---
// Replaces the old separate ahead/behind row + MergeStatusBanner.

function BranchStatus({
  meta,
  git,
  onSendMessage,
}: {
  meta: SessionMetadata;
  git: ReturnType<typeof useGitActions>;
  onSendMessage?: (prompt: string) => void;
}) {
  const isBusy = meta.state === "running";
  const ahead = meta.commitsAhead ?? 0;
  const behind = meta.commitsBehind ?? 0;

  if (meta.worktreeMerged && ahead === 0 && behind === 0) {
    return (
      <div className="flex items-center gap-2 text-xs text-success/80">
        <CheckCircle2 className="h-3.5 w-3.5 shrink-0" />
        Merged
      </div>
    );
  }

  // Conflicts — needs attention
  if (meta.mergeStatus === "conflicts") {
    return (
      <div className="space-y-1.5">
        <div className="flex items-center gap-2 text-xs text-warning">
          <AlertTriangle className="h-3.5 w-3.5 shrink-0" />
          <span>{meta.mergeConflictFiles?.length ?? 0} conflicting files</span>
        </div>
        {meta.mergeConflictFiles && meta.mergeConflictFiles.length > 0 && (
          <ul className="text-[11px] text-warning/70 space-y-0.5 pl-5.5">
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
            className="h-6 px-2 text-xs text-warning hover:text-warning hover:bg-warning/10"
            onClick={() => {
              const files = meta.mergeConflictFiles?.join(", ") ?? "";
              onSendMessage?.(
                `This is a git worktree. Rebase onto the local project HEAD (not origin). Get it via: main_wt=$(git worktree list --porcelain | head -1 | sed 's/worktree //') && git rebase $(git -C "$main_wt" rev-parse HEAD). Resolve conflicts in: ${files}`,
              );
            }}
          >
            Rebase & Resolve
          </Button>
        )}
      </div>
    );
  }

  // No commits ahead — nothing interesting to show
  if (ahead === 0 && behind === 0) return null;

  // Build compact status line: "↑2 ahead  ↓1 behind" with action
  return (
    <div className="space-y-1.5">
      <div className="flex items-center gap-3 text-xs">
        {ahead > 0 && (
          <span className="flex items-center gap-1 text-muted-foreground">
            <ArrowUp className="h-3 w-3" />
            {ahead} ahead
          </span>
        )}
        {behind > 0 && (
          <span className="flex items-center gap-1 text-primary/80">
            <ArrowDown className="h-3 w-3" />
            {behind} behind
          </span>
        )}
        {meta.mergeStatus === "clean" && ahead > 0 && (
          <CheckCircle2 className="h-3 w-3 text-success/70 ml-auto shrink-0" />
        )}
      </div>

      {/* Actions based on state */}
      {!isBusy && (
        <div className="flex items-center gap-1.5">
          {behind > 0 && (
            <Button
              variant="ghost"
              size="sm"
              className="h-6 px-2 text-xs text-primary hover:bg-primary/10"
              onClick={git.handleRebase}
              disabled={git.rebasing}
            >
              {git.rebasing ? <Loader2 className="h-3 w-3 animate-spin" /> : null}
              Rebase
            </Button>
          )}
          {ahead > 0 && (
            <MergeDropdown
              git={git}
              className={
                meta.mergeStatus === "clean" ? "text-success hover:bg-success/10" : undefined
              }
            />
          )}
        </div>
      )}
    </div>
  );
}

// --- Uncommitted files section ---

const uncommittedFileIconMap = {
  modified: FileText,
  added: FilePlus,
  deleted: FileMinus,
  renamed: FileText,
  untracked: FileQuestion,
} as const;

function UncommittedFileIcon({ status }: { status: string }) {
  const Icon = uncommittedFileIconMap[status as keyof typeof uncommittedFileIconMap] ?? FileText;
  return <Icon className="h-3 w-3 shrink-0" />;
}

function UncommittedSection({
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
  const isBusy = meta.state === "running";

  if (!git.uncommittedFiles || git.uncommittedFiles.length === 0) return null;

  const handleCommit = () => {
    if (onSendMessage) {
      onSendMessage(
        "Commit all changes. Stage everything and write a clear commit message based on what you changed.",
      );
    } else {
      onOpenDialog?.("commit");
    }
  };

  return (
    <div className="space-y-1.5">
      <div className="flex items-center gap-1.5">
        <button
          type="button"
          onClick={git.toggleUncommittedExpanded}
          className="flex items-center gap-1.5 text-xs text-warning/80 hover:text-warning transition-colors min-w-0"
        >
          {git.uncommittedExpanded ? (
            <ChevronDown className="h-3 w-3 shrink-0" />
          ) : (
            <ChevronRight className="h-3 w-3 shrink-0" />
          )}
          <span>{git.uncommittedFiles.length} uncommitted</span>
        </button>
        {!isBusy && (
          <Button
            variant="ghost"
            size="sm"
            className="h-6 px-2 text-xs ml-auto shrink-0"
            onClick={handleCommit}
            disabled={git.committing}
          >
            {git.committing ? (
              <Loader2 className="h-3.5 w-3.5 animate-spin" />
            ) : (
              <GitCommitHorizontal className="h-3.5 w-3.5" />
            )}
            Commit
          </Button>
        )}
      </div>
      {git.uncommittedExpanded && (
        <ul className="text-[11px] space-y-0.5 pl-5">
          {git.uncommittedFiles.map((f) => (
            <li key={f.path} className="flex items-center gap-1.5 text-muted-foreground">
              <UncommittedFileIcon status={f.status} />
              <span className="font-mono truncate">{f.path}</span>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}

// --- Main git section ---

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
        {isWorktree && (
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

      {meta.branchMissing && <div className="text-xs text-destructive/80">Branch missing</div>}

      {/* 1. Uncommitted changes — highest priority, blocks other operations */}
      <UncommittedSection
        meta={meta}
        git={git}
        onSendMessage={onSendMessage}
        onOpenDialog={onOpenDialog}
      />

      {/* 2. Branch status: ahead/behind + merge/rebase */}
      {isWorktree && !meta.branchMissing && (
        <BranchStatus meta={meta} git={git} onSendMessage={onSendMessage} />
      )}

      {/* 3. PR */}
      {meta.prUrl ? (
        <a
          href={meta.prUrl}
          target="_blank"
          rel="noreferrer"
          className="flex items-center gap-1.5 text-xs text-primary/80 hover:text-primary transition-colors"
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
            <Loader2 className="h-3.5 w-3.5 animate-spin" />
          ) : (
            <ExternalLink className="h-3.5 w-3.5" />
          )}
          Create PR
        </Button>
      ) : null}
    </div>
  );
}

// --- Todos ---

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

// --- Panel container ---

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
  const isDirty = meta.hasUncommitted || meta.hasDirtyWorktree;
  const showGit = isWorktree || isDirty || !hasTodos;

  return (
    <div className="flex flex-col h-full bg-background">
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
        {showGit && (
          <GitSection
            meta={meta}
            git={git}
            onSendMessage={onSendMessage}
            onOpenDialog={onOpenDialog}
          />
        )}

        {hasTodos && (
          <>
            {showGit && <div className="border-t" />}
            <TodoSection todos={todos} />
          </>
        )}
      </div>
    </div>
  );
}

// --- Collapsed strip ---

interface CollapsedSessionStripProps {
  meta: SessionMetadata;
  todos: TodoItem[] | null;
  onExpand: () => void;
}

export function CollapsedSessionStrip({ meta, todos, onExpand }: CollapsedSessionStripProps) {
  const hasTodos = todos !== null && todos.length > 0;
  const completed = hasTodos ? todos.filter((t) => t.status === "completed").length : 0;

  let mergeIcon = null;
  if (meta.worktreeMerged && (meta.commitsAhead ?? 0) === 0) {
    mergeIcon = <CheckCircle2 className="h-3.5 w-3.5 text-success/70" />;
  } else if (meta.mergeStatus === "conflicts") {
    mergeIcon = <AlertTriangle className="h-3.5 w-3.5 text-warning/70" />;
  } else if (meta.mergeStatus === "clean" && (meta.commitsAhead ?? 0) > 0) {
    mergeIcon = <CheckCircle2 className="h-3.5 w-3.5 text-success/50" />;
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
