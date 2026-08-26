import { FileX2 } from "lucide-react";
import { useCallback, useEffect, useRef, useState } from "react";
import type { DiffLine } from "~/lib/diff-lines";
import { buildDiffQuote } from "~/lib/diff-quote";
import { FileDiffSection, type LineSelection } from "./FileDiffSection";
import type { MergedFile } from "./types";

/**
 * Files collapsed from the start once a change touches this many of them. A
 * scroll that opens on forty expanded diffs is not a list of what changed.
 */
const COLLAPSE_ALL_ABOVE = 8;

interface FileDiffListProps {
  files: MergedFile[];
  /** Keyed by scope, so switching scopes does not inherit the other's folds. */
  scopeKey: string;
  truncated: boolean;
  wrap: boolean;
  /** Set by the toolbar's collapse-all; a per-file toggle overrides it. */
  collapseAll: boolean;
  onQuote?: (text: string) => void;
  /** Working-tree scope only. */
  onDiscardFile?: (path: string) => void;
  /** Reveals and expands one file — the transcript's "view this file" link. */
  revealFile?: string | null;
  onRevealConsumed?: () => void;
}

/**
 * Every changed file in one scroll, each one foldable, headers sticking as you
 * pass them.
 *
 * This replaced a file list beside a diff pane. The pane model needs width the
 * dock does not have: at 380px the list took three quarters of it and the diff
 * read through a slot. One column asks nothing of the layout and reads the same
 * maximized as it does docked.
 *
 * Only one file holds a line selection at a time. Two selections in two files
 * would mean two answers to "ask about this", and the composer takes one thing
 * at a time.
 */
export function FileDiffList({
  files,
  scopeKey,
  truncated,
  wrap,
  collapseAll,
  onQuote,
  onDiscardFile,
  revealFile,
  onRevealConsumed,
}: FileDiffListProps) {
  const [overrides, setOverrides] = useState<Record<string, boolean>>({});
  const [selection, setSelection] = useState<{ path: string; range: LineSelection } | null>(null);
  const scrollRef = useRef<HTMLDivElement>(null);

  // A fold is an opinion about the scope you formed it in. Switching scopes
  // shows a different set of files; carrying folds across would collapse ones
  // the reader has never seen.
  // biome-ignore lint/correctness/useExhaustiveDependencies: keyed reset
  useEffect(() => {
    setOverrides({});
    setSelection(null);
  }, [scopeKey]);

  const defaultCollapsed = collapseAll || files.length > COLLAPSE_ALL_ABOVE;

  const toggle = useCallback((path: string, current: boolean) => {
    setOverrides((prev) => ({ ...prev, [path]: !current }));
  }, []);

  const quoteLines = useCallback(
    (path: string, lines: readonly DiffLine[], from: number, to: number) => {
      onQuote?.(buildDiffQuote(path, lines, from, to));
      setSelection(null);
    },
    [onQuote],
  );

  // An external "show me this file" both opens the file and scrolls to it.
  useEffect(() => {
    if (!revealFile) return;
    setOverrides((prev) => ({ ...prev, [revealFile]: false }));
    const node = scrollRef.current?.querySelector<HTMLElement>(
      `[data-file-path="${CSS.escape(revealFile)}"]`,
    );
    node?.scrollIntoView({ block: "start" });
    onRevealConsumed?.();
  }, [revealFile, onRevealConsumed]);

  if (files.length === 0) {
    return (
      <div className="flex flex-1 flex-col items-center justify-center gap-2 text-muted-foreground text-sm">
        <FileX2 className="size-5 text-muted-foreground-faint" />
        No file changes in this view.
      </div>
    );
  }

  return (
    <div ref={scrollRef} className="min-h-0 flex-1 overflow-y-auto">
      {truncated && (
        <p className="border-b bg-warning/10 px-3 py-1 text-[10px] text-warning">
          This diff was truncated — the changes shown are incomplete.
        </p>
      )}
      {files.map((file) => {
        const collapsed = overrides[file.path] ?? defaultCollapsed;
        return (
          <div key={file.path} data-file-path={file.path}>
            <FileDiffSection
              file={file}
              collapsed={collapsed}
              onToggle={() => toggle(file.path, collapsed)}
              wrap={wrap}
              selection={selection?.path === file.path ? selection.range : null}
              onSelectionChange={(range) => setSelection(range ? { path: file.path, range } : null)}
              {...(onQuote
                ? {
                    onQuoteLines: (lines: readonly DiffLine[], from: number, to: number) =>
                      quoteLines(file.path, lines, from, to),
                    onAskAboutFile: () => onQuote(`\`${file.path}\`\n`),
                  }
                : {})}
              {...(onDiscardFile ? { onDiscard: () => onDiscardFile(file.path) } : {})}
            />
          </div>
        );
      })}
    </div>
  );
}
