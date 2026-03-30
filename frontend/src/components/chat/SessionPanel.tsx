import {
  AlertTriangle,
  ArrowDown,
  ArrowRight,
  ArrowUp,
  CheckCircle2,
  ChevronDown,
  ChevronRight,
  Circle,
  ExternalLink,
  FileMinus,
  FilePlus,
  FileQuestion,
  FileText,
  GitBranch,
  GitCommitHorizontal,
  GitMerge,
  GitPullRequestArrow,
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
import { Tooltip, TooltipContent, TooltipTrigger } from "~/components/ui/tooltip";
import type { useGitActions } from "~/hooks/useGitActions";
import { cn } from "~/lib/utils";
import type { SessionMetadata, TodoItem } from "~/stores/chat-store";
import { TodoItemRow } from "./TodoPanel";

interface SessionPanelProps {
  meta: SessionMetadata;
  todos: TodoItem[] | null;
  git: ReturnType<typeof useGitActions>;
  mainBranch?: string;
  onCollapse: () => void;
  onSendMessage?: (prompt: string) => void;
  onOpenDialog?: (dialog: "pr" | "commit") => void;
}

// --- Section header with rule line ---

function SectionHeader({
  label,
  action,
}: {
  label: string;
  action?: React.ReactNode;
}) {
  return (
    <div className="flex items-center gap-2">
      <span className="text-[11px] font-semibold text-muted-foreground/70 uppercase tracking-widest shrink-0">
        {label}
      </span>
      <div className="flex-1 border-t border-border/40" />
      {action}
    </div>
  );
}

// --- Merge dropdown ---

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
        <Button variant="ghost" size="xs" className={className} disabled={git.merging}>
          {git.merging ? (
            <Loader2 className="h-3 w-3 animate-spin" />
          ) : (
            <GitMerge className="h-3 w-3" />
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

  // Conflicts
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
            size="xs"
            className="text-warning hover:text-warning hover:bg-warning/10"
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

  if (ahead === 0 && behind === 0) return null;

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

      {!isBusy && (
        <div className="flex items-center gap-1.5">
          {behind > 0 && (
            <Button
              variant="ghost"
              size="xs"
              className="text-primary hover:bg-primary/10"
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

const uncommittedFileColorMap: Record<string, string> = {
  modified: "text-warning/70",
  added: "text-success/70",
  deleted: "text-destructive/70",
  renamed: "text-primary/70",
  untracked: "text-muted-foreground/50",
};

function UncommittedFileIcon({ status }: { status: string }) {
  const Icon = uncommittedFileIconMap[status as keyof typeof uncommittedFileIconMap] ?? FileText;
  const color = uncommittedFileColorMap[status] ?? "text-muted-foreground/50";
  return <Icon className={`h-3 w-3 shrink-0 ${color}`} />;
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
            size="xs"
            className="ml-auto shrink-0"
            onClick={handleCommit}
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
  mainBranch,
  onSendMessage,
  onOpenDialog,
}: {
  meta: SessionMetadata;
  git: ReturnType<typeof useGitActions>;
  mainBranch?: string;
  onSendMessage?: (prompt: string) => void;
  onOpenDialog?: (dialog: "pr" | "commit") => void;
}) {
  const isWorktree = !!meta.worktreeBranch;
  const isBusy = meta.state === "running";

  return (
    <div className="space-y-3">
      <SectionHeader
        label="Git"
        action={
          isWorktree ? (
            <Button
              variant="ghost"
              size="icon-xs"
              onClick={git.handleRefreshGit}
              disabled={git.refreshingGit}
              className="text-muted-foreground hover:text-foreground"
            >
              <RefreshCw className={cn("h-3 w-3", git.refreshingGit && "animate-spin")} />
            </Button>
          ) : undefined
        }
      />

      {/* Branch line */}
      {isWorktree && (
        <div className="rounded-md bg-muted/30 px-2.5 py-1.5 flex items-center gap-1.5 text-xs text-muted-foreground">
          <GitBranch className="h-3 w-3 shrink-0" />
          <span className="font-mono truncate">{meta.worktreeBranch}</span>
          <ArrowRight className="h-3 w-3 shrink-0 text-muted-foreground/50" />
          <span className="font-mono">{mainBranch || "main"}</span>
        </div>
      )}

      {meta.branchMissing && <div className="text-xs text-destructive/80">Branch missing</div>}

      <UncommittedSection
        meta={meta}
        git={git}
        onSendMessage={onSendMessage}
        onOpenDialog={onOpenDialog}
      />

      {isWorktree && !meta.branchMissing && (
        <BranchStatus meta={meta} git={git} onSendMessage={onSendMessage} />
      )}

      {/* PR */}
      {meta.prUrl ? (
        <a
          href={meta.prUrl}
          target="_blank"
          rel="noreferrer"
          className="flex items-center gap-2 rounded-md bg-primary/5 px-2.5 py-1.5 text-xs text-primary/80 hover:text-primary hover:bg-primary/10 transition-colors"
        >
          <GitPullRequestArrow className="h-3.5 w-3.5 shrink-0" />
          <span className="truncate flex-1">Pull Request</span>
          <ExternalLink className="h-3 w-3 shrink-0 opacity-50" />
        </a>
      ) : isWorktree && !isBusy ? (
        <Button
          variant="outline"
          size="xs"
          className="w-full justify-start"
          onClick={() => onOpenDialog?.("pr")}
          disabled={git.creatingPR}
        >
          {git.creatingPR ? (
            <Loader2 className="h-3 w-3 animate-spin" />
          ) : (
            <GitPullRequestArrow className="h-3 w-3" />
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
  const pct = todos.length > 0 ? (completed / todos.length) * 100 : 0;

  return (
    <div className="space-y-2">
      <SectionHeader
        label="Todos"
        action={
          <span className="text-[11px] text-muted-foreground/50 tabular-nums">
            {completed}/{todos.length}
          </span>
        }
      />
      <div className="h-1 rounded-full bg-muted overflow-hidden">
        <div
          className="h-full rounded-full bg-success transition-all duration-300"
          style={{ width: `${pct}%` }}
        />
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
  mainBranch,
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
            mainBranch={mainBranch}
            onSendMessage={onSendMessage}
            onOpenDialog={onOpenDialog}
          />
        )}

        {hasTodos && <TodoSection todos={todos} />}
      </div>
    </div>
  );
}

// --- Collapsed strip ---

interface CollapsedSessionStripProps {
  meta: SessionMetadata;
  todos: TodoItem[] | null;
  uncommittedCount: number;
  onExpand: () => void;
}

function StripTooltip({
  label,
  children,
}: {
  label: string;
  children: React.ReactNode;
}) {
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <div className="flex items-center justify-center">{children}</div>
      </TooltipTrigger>
      <TooltipContent side="left" className="text-xs">
        {label}
      </TooltipContent>
    </Tooltip>
  );
}

export function CollapsedSessionStrip({
  meta,
  todos,
  uncommittedCount,
  onExpand,
}: CollapsedSessionStripProps) {
  const hasTodos = todos !== null && todos.length > 0;
  const completed = hasTodos ? todos.filter((t) => t.status === "completed").length : 0;
  const ahead = meta.commitsAhead ?? 0;
  const behind = meta.commitsBehind ?? 0;
  const hasConflicts = meta.mergeStatus === "conflicts";
  const isMerged = meta.worktreeMerged && ahead === 0 && behind === 0;
  const hasGitContent = !!meta.worktreeBranch || uncommittedCount > 0;
  const hasPR = !!meta.prUrl;

  return (
    <button
      type="button"
      onClick={onExpand}
      className="w-9 border-l flex flex-col items-center py-3 gap-2 shrink-0 hover:bg-muted/50 transition-colors"
    >
      {/* Branch icon — colored by state */}
      {meta.worktreeBranch && !isMerged && !hasConflicts && (
        <StripTooltip label={meta.worktreeBranch}>
          <GitBranch className="h-3.5 w-3.5 text-muted-foreground/50" />
        </StripTooltip>
      )}

      {/* Merged */}
      {isMerged && (
        <StripTooltip label="Merged">
          <CheckCircle2 className="h-3.5 w-3.5 text-success/70" />
        </StripTooltip>
      )}

      {/* Conflicts */}
      {hasConflicts && (
        <StripTooltip label={`${meta.mergeConflictFiles?.length ?? 0} conflicts`}>
          <AlertTriangle className="h-3.5 w-3.5 text-warning/70" />
        </StripTooltip>
      )}

      {/* Uncommitted count pill */}
      {uncommittedCount > 0 && (
        <StripTooltip label={`${uncommittedCount} uncommitted`}>
          <span className="flex items-center gap-0.5 rounded-full bg-warning/15 px-1.5 py-0.5 text-[10px] text-warning">
            <Circle className="size-1.5 fill-current" />
            {uncommittedCount}
          </span>
        </StripTooltip>
      )}

      {/* Ahead pill */}
      {ahead > 0 && !isMerged && (
        <StripTooltip label={`${ahead} ahead`}>
          <span className="flex items-center gap-0.5 rounded-full bg-success/15 px-1.5 py-0.5 text-[10px] text-success">
            <ArrowUp className="size-2" />
            {ahead}
          </span>
        </StripTooltip>
      )}

      {/* Behind pill */}
      {behind > 0 && (
        <StripTooltip label={`${behind} behind`}>
          <span className="flex items-center gap-0.5 rounded-full bg-primary/15 px-1.5 py-0.5 text-[10px] text-primary">
            <ArrowDown className="size-2" />
            {behind}
          </span>
        </StripTooltip>
      )}

      {/* PR indicator */}
      {hasPR && (
        <StripTooltip label="Pull request">
          <GitPullRequestArrow className="h-3.5 w-3.5 text-primary/60" />
        </StripTooltip>
      )}

      {/* Divider between git and todos */}
      {hasGitContent && hasTodos && <div className="w-4 border-t border-border/40" />}

      {/* Todos */}
      {hasTodos && (
        <StripTooltip label={`${completed}/${todos.length} todos`}>
          <div className="flex flex-col items-center gap-1">
            <ListTodo className="h-3.5 w-3.5 text-muted-foreground/50" />
            <span className="text-[10px] text-muted-foreground/60 tabular-nums">
              {completed}/{todos.length}
            </span>
          </div>
        </StripTooltip>
      )}
    </button>
  );
}
