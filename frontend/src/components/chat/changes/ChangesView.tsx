import { ArrowDown, ArrowUp, CheckCircle2, FileX2, GitMerge, Loader2 } from "lucide-react";
import { useCallback, useEffect, useMemo, useState } from "react";
import { toast } from "sonner";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "~/components/ui/alert-dialog";
import type { useGitActions } from "~/hooks/git/useGitActions";
import type { useProjectGitActions } from "~/hooks/git/useProjectGitActions";
import { useWebSocket } from "~/hooks/useWebSocket";
import { type DiffResult, discardSessionFile } from "~/lib/session/actions";
import { cn, getErrorMessage } from "~/lib/utils";
import type { ProjectGitStatus } from "~/stores/app-store";
import type { SessionMetadata } from "~/stores/chat-store";
import type { SessionState } from "~/stores/chat-types";
import { ChangesToolbar, type DiffScope } from "./ChangesToolbar";
import { CommitsView } from "./CommitsView";
import { FileDiffList } from "./FileDiffList";
import { GitStatusBar } from "./GitStatusBar";
import { diffTotals, filesForScope } from "./types";

type SubTab = "files" | "commits";

interface ChangesViewProps {
  meta: SessionMetadata;
  git: ReturnType<typeof useGitActions>;
  mainBranch?: string;
  projectGitStatus?: ProjectGitStatus;
  projectGitActions?: ReturnType<typeof useProjectGitActions>;
  committedDiff: DiffResult | null;
  uncommittedDiff: DiffResult | null;
  sessionState?: SessionState;
  onSendMessage: (prompt: string) => void;
  onOpenDialog: (dialog: "pr" | "commit") => void;
  /** Writes into the composer and stops there. Absent where there is none. */
  onQuoteToComposer?: (text: string) => void;
  expandFile?: string | null;
  onExpandFileConsumed?: () => void;
}

/**
 * What this session changed, and what you can do about it.
 *
 * Two sub-tabs, because they answer different questions: **Files** is the diff
 * — one scroll, every file foldable — and **Commits** is the history around it,
 * the PR, the rebase, the conflicts. Files leads when there are any, because
 * that is what the tab is named after.
 *
 * The old two-pane arrangement (a file list beside one file's diff) is gone;
 * see `FileDiffList` for why a dock-width column cannot afford it.
 */
export function ChangesView({
  meta,
  git,
  mainBranch,
  projectGitStatus,
  projectGitActions,
  committedDiff,
  uncommittedDiff,
  sessionState,
  onSendMessage,
  onOpenDialog,
  onQuoteToComposer,
  expandFile,
  onExpandFileConsumed,
}: ChangesViewProps) {
  const ws = useWebSocket();
  const [scope, setScope] = useState<DiffScope>("session");
  // The path awaiting confirmation. Discarding uncommitted work is the one
  // irreversible thing in this view, so it never happens on the first click.
  const [pendingDiscard, setPendingDiscard] = useState<string | null>(null);
  const [discarding, setDiscarding] = useState(false);
  const [collapseAll, setCollapseAll] = useState(false);
  const [wrap, setWrap] = useState(false);
  const [subTab, setSubTab] = useState<SubTab>("files");

  const isWorktree = !!meta.worktreeBranch;
  const sessionFiles = useMemo(
    () => filesForScope("session", committedDiff, uncommittedDiff),
    [committedDiff, uncommittedDiff],
  );
  const files = useMemo(
    () =>
      scope === "session" ? sessionFiles : filesForScope("working", committedDiff, uncommittedDiff),
    [scope, sessionFiles, committedDiff, uncommittedDiff],
  );
  const totals = useMemo(() => diffTotals(files), [files]);

  // A link from the transcript names a file, so the view has to be showing the
  // scope that contains it before it can reveal it.
  useEffect(() => {
    if (!expandFile) return;
    setSubTab("files");
    if (
      !files.some((f) => f.path === expandFile) &&
      sessionFiles.some((f) => f.path === expandFile)
    )
      setScope("session");
  }, [expandFile, files, sessionFiles]);

  const handleQuote = useCallback(
    (text: string) => {
      onQuoteToComposer?.(text);
    },
    [onQuoteToComposer],
  );

  const confirmDiscard = useCallback(async () => {
    if (!pendingDiscard) return;
    setDiscarding(true);
    try {
      await discardSessionFile(ws, meta.id, pendingDiscard);
      setPendingDiscard(null);
      await git.handleRefreshGit();
    } catch (err) {
      toast.error(getErrorMessage(err, "Failed to discard changes"));
    } finally {
      setDiscarding(false);
    }
  }, [ws, meta.id, pendingDiscard, git]);

  const truncated = (committedDiff?.truncated ?? false) || (uncommittedDiff?.truncated ?? false);
  const hasFiles = sessionFiles.length > 0 || (uncommittedDiff?.files.length ?? 0) > 0;
  const isMerged =
    meta.worktreeMerged && (meta.commitsAhead ?? 0) === 0 && (meta.commitsBehind ?? 0) === 0;
  const hasGitContent =
    isWorktree ||
    !!meta.hasUncommitted ||
    !!meta.hasDirtyWorktree ||
    (projectGitStatus?.aheadRemote ?? 0) > 0 ||
    (projectGitStatus?.behindRemote ?? 0) > 0;

  const ahead = meta.commitsAhead ?? 0;
  const behind = meta.commitsBehind ?? 0;
  const main = mainBranch || "main";

  if (!hasFiles && !hasGitContent) {
    return <EmptyState sessionState={sessionState} worktreeMerged={meta.worktreeMerged} />;
  }

  return (
    <div className="flex min-h-0 flex-1 flex-col overflow-hidden">
      <GitStatusBar
        meta={meta}
        git={git}
        mainBranch={mainBranch}
        projectGitStatus={projectGitStatus}
        onSendMessage={onSendMessage}
        onOpenDialog={onOpenDialog}
      />

      {isMerged && !hasFiles && (
        <div className="flex flex-1 flex-col items-center justify-center gap-2 text-muted-foreground text-sm">
          <GitMerge className="size-5 text-success/60" />
          All changes merged.
        </div>
      )}

      {hasFiles && (
        <>
          <SubTabBar
            activeTab={subTab}
            onTabChange={setSubTab}
            fileCount={files.length}
            ahead={ahead}
            behind={behind}
          />

          {subTab === "files" && (
            <>
              <ChangesToolbar
                scope={scope}
                onScopeChange={setScope}
                // Without a worktree there is no base commit to measure from, so
                // the two scopes are the same diff under two names.
                scopeChoice={isWorktree}
                isWorktree={isWorktree}
                fileCount={files.length}
                insertions={totals.insertions}
                deletions={totals.deletions}
                allCollapsed={collapseAll}
                onToggleCollapseAll={() => setCollapseAll((v) => !v)}
                wrap={wrap}
                onWrapChange={setWrap}
              />
              <FileDiffList
                files={files}
                scopeKey={scope}
                truncated={truncated}
                wrap={wrap}
                collapseAll={collapseAll}
                {...(onQuoteToComposer ? { onQuote: handleQuote } : {})}
                {...(scope === "working" ? { onDiscardFile: setPendingDiscard } : {})}
                revealFile={expandFile ?? null}
                {...(onExpandFileConsumed ? { onRevealConsumed: onExpandFileConsumed } : {})}
              />
            </>
          )}

          {subTab === "commits" && (
            <CommitsView
              meta={meta}
              git={git}
              mainBranch={main}
              projectGitStatus={projectGitStatus}
              projectGitActions={projectGitActions}
              onSendMessage={onSendMessage}
            />
          )}
        </>
      )}

      {!hasFiles && hasGitContent && !isMerged && (
        <>
          <CommitsView
            meta={meta}
            git={git}
            mainBranch={main}
            projectGitStatus={projectGitStatus}
            projectGitActions={projectGitActions}
            onSendMessage={onSendMessage}
          />
          <div className="flex flex-1 flex-col items-center justify-center gap-2 text-muted-foreground text-sm">
            <FileX2 className="size-5 text-muted-foreground-faint" />
            No file changes yet.
          </div>
        </>
      )}

      <AlertDialog
        open={pendingDiscard !== null}
        onOpenChange={(open) => !open && setPendingDiscard(null)}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Discard changes to this file?</AlertDialogTitle>
            <AlertDialogDescription>
              <span className="font-mono text-xs">{pendingDiscard}</span> goes back to its committed
              state, or is deleted if it was never committed. There is no undo.
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>Cancel</AlertDialogCancel>
            <AlertDialogAction
              onClick={(event) => {
                // Keep the dialog up while the discard runs, so a slow git call
                // cannot read as "nothing happened".
                event.preventDefault();
                void confirmDiscard();
              }}
              disabled={discarding}
            >
              {discarding ? "Discarding\u2026" : "Discard"}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}

// ---------------------------------------------------------------------------
// Sub-tab bar
// ---------------------------------------------------------------------------

function subTabClass(active: boolean) {
  return cn(
    "flex cursor-pointer items-center gap-1.5 border-b-2 px-3 py-1.5 text-xs transition-colors",
    active
      ? "border-b-primary text-foreground"
      : "border-b-transparent text-muted-foreground hover:border-b-muted-foreground/30 hover:text-foreground",
  );
}

function SubTabBar({
  activeTab,
  onTabChange,
  fileCount,
  ahead,
  behind,
}: {
  activeTab: SubTab;
  onTabChange: (tab: SubTab) => void;
  fileCount: number;
  ahead: number;
  behind: number;
}) {
  return (
    <div className="flex shrink-0 items-center border-b">
      <button
        type="button"
        onClick={() => onTabChange("files")}
        className={subTabClass(activeTab === "files")}
      >
        Files
        <span className="text-[11px] text-muted-foreground tabular-nums">{fileCount}</span>
      </button>
      <button
        type="button"
        onClick={() => onTabChange("commits")}
        className={subTabClass(activeTab === "commits")}
      >
        Commits
        {(ahead > 0 || behind > 0) && (
          <span className="flex items-center gap-1 text-[11px]">
            {ahead > 0 && (
              <span className="flex items-center gap-0.5 text-success">
                <ArrowUp className="size-2.5" />
                {ahead}
              </span>
            )}
            {behind > 0 && (
              <span className="flex items-center gap-0.5 text-orange">
                <ArrowDown className="size-2.5" />
                {behind}
              </span>
            )}
          </span>
        )}
      </button>
    </div>
  );
}

// ---------------------------------------------------------------------------
// Empty state
// ---------------------------------------------------------------------------

function EmptyState({
  sessionState,
  worktreeMerged,
}: {
  sessionState?: SessionState;
  worktreeMerged?: boolean;
}) {
  if (worktreeMerged) {
    return (
      <div className="flex flex-1 flex-col items-center justify-center gap-2 text-muted-foreground text-sm">
        <CheckCircle2 className="size-5 text-success/60" />
        All changes merged into main.
      </div>
    );
  }
  if (sessionState === "running" || sessionState === "idle") {
    return (
      <div className="flex flex-1 flex-col items-center justify-center gap-2 text-muted-foreground text-sm">
        <Loader2 className="size-5 animate-spin text-muted-foreground-dim" />
        Changes will appear as the session works.
      </div>
    );
  }
  if (sessionState === "stopped" || sessionState === "done" || sessionState === "failed") {
    return (
      <div className="flex flex-1 flex-col items-center justify-center gap-2 text-muted-foreground text-sm">
        <FileX2 className="size-5 text-muted-foreground-faint" />
        No changes were made in this session.
      </div>
    );
  }
  return (
    <div className="flex flex-1 items-center justify-center text-muted-foreground text-sm">
      No changes detected.
    </div>
  );
}
