import { ArrowDown, ArrowUp, CheckCircle2, FileX2, GitMerge, Loader2 } from "lucide-react";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import type { useGitActions } from "~/hooks/git/useGitActions";
import type { useProjectGitActions } from "~/hooks/git/useProjectGitActions";
import { useIsMobile } from "~/hooks/useIsMobile";
import type { DiffResult } from "~/lib/session/actions";
import { cn } from "~/lib/utils";
import type { ProjectGitStatus } from "~/stores/app-store";
import type { SessionMetadata } from "~/stores/chat-store";
import type { SessionState } from "~/stores/chat-types";
import { CommitsView } from "./CommitsView";
import { DiffViewer } from "./DiffViewer";
import { FileList } from "./FileList";
import { GitStatusBar } from "./GitStatusBar";
import { groupFiles, type MergedFile } from "./types";

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
  expandFile?: string | null;
  onExpandFileConsumed?: () => void;
}

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
  expandFile,
  onExpandFileConsumed,
}: ChangesViewProps) {
  const isMobile = useIsMobile();
  const [selectedFile, setSelectedFile] = useState<string | null>(null);
  const [subTab, setSubTab] = useState<SubTab>("commits");

  const { committed, uncommitted } = useMemo(
    () => groupFiles(committedDiff, uncommittedDiff),
    [committedDiff, uncommittedDiff],
  );
  const allFiles = useMemo(() => [...committed, ...uncommitted], [committed, uncommitted]);

  const fileMap = useMemo(() => {
    const m = new Map<string, MergedFile>();
    for (const f of allFiles) {
      m.set(f.path, f);
    }
    return m;
  }, [allFiles]);

  const selectedMergedFile = selectedFile ? (fileMap.get(selectedFile) ?? null) : null;

  // Auto-select first file when files change and nothing is selected
  const prevFilesRef = useRef(allFiles);
  useEffect(() => {
    if (allFiles !== prevFilesRef.current) {
      prevFilesRef.current = allFiles;
      const first = allFiles[0];
      if (first && (!selectedFile || !fileMap.has(selectedFile))) {
        setSelectedFile(first.path);
      }
    }
  }, [allFiles, selectedFile, fileMap]);

  // Handle expandFile prop (external navigation)
  useEffect(() => {
    if (expandFile && fileMap.has(expandFile)) {
      setSelectedFile(expandFile);
      setSubTab("files");
      onExpandFileConsumed?.();
    } else if (expandFile) {
      onExpandFileConsumed?.();
    }
  }, [expandFile, fileMap, onExpandFileConsumed]);

  const handleSelectFile = useCallback((path: string) => {
    setSelectedFile(path);
  }, []);

  const handleBack = useCallback(() => {
    setSelectedFile(null);
  }, []);

  // Keyboard navigation: arrow keys to move between files
  useEffect(() => {
    if (subTab !== "files" || allFiles.length === 0) return;

    function onKeyDown(e: KeyboardEvent) {
      if (e.key !== "ArrowUp" && e.key !== "ArrowDown") return;
      // Don't hijack if user is in an input/textarea
      const tag = (e.target as HTMLElement).tagName;
      if (tag === "INPUT" || tag === "TEXTAREA") return;

      e.preventDefault();
      const currentIdx = allFiles.findIndex((f) => f.path === selectedFile);
      const nextIdx =
        e.key === "ArrowDown"
          ? Math.min(currentIdx + 1, allFiles.length - 1)
          : Math.max(currentIdx - 1, 0);
      const next = allFiles[nextIdx];
      if (next) setSelectedFile(next.path);
    }

    window.addEventListener("keydown", onKeyDown);
    return () => window.removeEventListener("keydown", onKeyDown);
  }, [subTab, allFiles, selectedFile]);

  const truncated = (committedDiff?.truncated ?? false) || (uncommittedDiff?.truncated ?? false);
  const hasFiles = allFiles.length > 0;
  const isWorktree = !!meta.worktreeBranch;
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

  // Empty state: no files and no git content worth showing
  if (!hasFiles && !hasGitContent) {
    return <EmptyState sessionState={sessionState} worktreeMerged={meta.worktreeMerged} />;
  }

  return (
    <div className="flex-1 flex flex-col min-h-0 overflow-hidden">
      {/* Zone 1: Git status bar (branch + actions) */}
      <GitStatusBar
        meta={meta}
        git={git}
        mainBranch={mainBranch}
        projectGitStatus={projectGitStatus}
        onSendMessage={onSendMessage}
        onOpenDialog={onOpenDialog}
      />

      {/* Merged state */}
      {isMerged && !hasFiles && (
        <div className="flex-1 flex flex-col items-center justify-center gap-2 text-sm text-muted-foreground">
          <GitMerge className="h-5 w-5 text-success/60" />
          All changes merged.
        </div>
      )}

      {/* Sub-tab bar */}
      {hasFiles && (
        <>
          <SubTabBar
            activeTab={subTab}
            onTabChange={setSubTab}
            fileCount={allFiles.length}
            ahead={ahead}
            behind={behind}
          />

          {subTab === "files" && !isMobile && (
            <div className="flex-1 flex min-h-0">
              <div className="w-72 shrink-0 bg-muted/15 flex flex-col min-h-0">
                <FileList
                  committed={committed}
                  uncommitted={uncommitted}
                  selectedFile={selectedFile}
                  onSelectFile={handleSelectFile}
                  truncated={truncated}
                />
              </div>
              <DiffViewer file={selectedMergedFile} />
            </div>
          )}

          {subTab === "files" && isMobile && !selectedFile && (
            <div className="flex-1 flex flex-col min-h-0">
              <FileList
                committed={committed}
                uncommitted={uncommitted}
                selectedFile={null}
                onSelectFile={handleSelectFile}
                truncated={truncated}
              />
            </div>
          )}

          {subTab === "files" && isMobile && selectedFile && (
            <div className="flex-1 flex flex-col min-h-0">
              <button
                type="button"
                onClick={handleBack}
                className="flex items-center gap-1.5 px-3 py-1.5 text-xs text-muted-foreground hover:text-foreground border-b shrink-0 transition-colors"
              >
                Back to files
              </button>
              <DiffViewer file={selectedMergedFile} />
            </div>
          )}

          {subTab === "commits" && (
            <CommitsView
              meta={meta}
              git={git}
              mainBranch={main}
              projectGitStatus={projectGitStatus}
              projectGitActions={projectGitActions}
              onSendMessage={onSendMessage}
              onSelectFile={handleSelectFile}
            />
          )}
        </>
      )}

      {/* No files but git content */}
      {!hasFiles && hasGitContent && !isMerged && (
        <>
          <CommitsView
            meta={meta}
            git={git}
            mainBranch={main}
            projectGitStatus={projectGitStatus}
            projectGitActions={projectGitActions}
            onSendMessage={onSendMessage}
            onSelectFile={handleSelectFile}
          />
          <div className="flex-1 flex flex-col items-center justify-center gap-2 text-sm text-muted-foreground">
            <FileX2 className="h-5 w-5 text-muted-foreground-faint" />
            No file changes yet.
          </div>
        </>
      )}
    </div>
  );
}

// ---------------------------------------------------------------------------
// Sub-tab bar
// ---------------------------------------------------------------------------

function subTabClass(active: boolean) {
  return cn(
    "flex items-center gap-1.5 px-3 py-1.5 text-xs transition-colors cursor-pointer border-b-2",
    active
      ? "text-foreground border-b-primary"
      : "text-muted-foreground border-b-transparent hover:text-foreground hover:border-b-muted-foreground/30",
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
    <div className="flex items-center border-b shrink-0">
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
                <ArrowUp className="h-2.5 w-2.5" />
                {ahead}
              </span>
            )}
            {behind > 0 && (
              <span className="flex items-center gap-0.5 text-orange">
                <ArrowDown className="h-2.5 w-2.5" />
                {behind}
              </span>
            )}
          </span>
        )}
      </button>
      <button
        type="button"
        onClick={() => onTabChange("files")}
        className={subTabClass(activeTab === "files")}
      >
        Files
        <span className="text-[11px] tabular-nums text-muted-foreground">{fileCount}</span>
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
      <div className="flex-1 flex flex-col items-center justify-center gap-2 text-sm text-muted-foreground">
        <CheckCircle2 className="h-5 w-5 text-success/60" />
        All changes merged into main.
      </div>
    );
  }
  if (sessionState === "running" || sessionState === "idle") {
    return (
      <div className="flex-1 flex flex-col items-center justify-center gap-2 text-sm text-muted-foreground">
        <Loader2 className="h-5 w-5 animate-spin text-muted-foreground-dim" />
        Changes will appear as the session works.
      </div>
    );
  }
  if (sessionState === "stopped" || sessionState === "done" || sessionState === "failed") {
    return (
      <div className="flex-1 flex flex-col items-center justify-center gap-2 text-sm text-muted-foreground">
        <FileX2 className="h-5 w-5 text-muted-foreground-faint" />
        No changes were made in this session.
      </div>
    );
  }
  return (
    <div className="flex-1 flex items-center justify-center text-sm text-muted-foreground">
      No changes detected.
    </div>
  );
}
