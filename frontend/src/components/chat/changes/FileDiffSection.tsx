import { ChevronDown, ChevronRight, Copy, MessageSquareQuote, MoreHorizontal } from "lucide-react";
import { memo, useMemo } from "react";
import { statusIcon } from "~/components/chat/DiffView";
import { FilePath } from "~/components/chat/git/FilePath";
import { Button } from "~/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "~/components/ui/dropdown-menu";
import { type DiffLine, diffNote, hasRenderableDiff, parseDiffLines } from "~/lib/diff-lines";
import { cn } from "~/lib/utils";
import type { MergedFile } from "./types";

export interface LineSelection {
  from: number;
  to: number;
}

interface FileDiffSectionProps {
  file: MergedFile;
  collapsed: boolean;
  onToggle: () => void;
  wrap: boolean;
  /** Non-null only for the one file that currently holds a selection. */
  selection: LineSelection | null;
  onSelectionChange: (selection: LineSelection | null) => void;
  /** Quote the selected lines into the composer. Absent where there is no composer. */
  onQuoteLines?: (lines: readonly DiffLine[], from: number, to: number) => void;
  onAskAboutFile?: () => void;
  /** Working-tree scope only, and confirmed by the caller. */
  onDiscard?: () => void;
}

const kindRowClass: Record<DiffLine["kind"], string> = {
  add: "bg-success/10 text-success",
  del: "bg-destructive/10 text-destructive",
  context: "text-muted-foreground",
  hunk: "bg-primary/5 text-primary",
  meta: "",
};

/**
 * One file's changes: a header that stays put while you scroll its diff, and
 * the diff itself.
 *
 * The header is the whole control — clicking anywhere but the menu folds the
 * file away — because in a dock-width column a separate chevron target is a
 * 12px hit area next to a 300px one that does nothing.
 *
 * Lines are addressed by delegation rather than by making each one a button:
 * a 2000-line diff would otherwise be 2000 tab stops in front of the composer.
 * Keyboard readers get the file-level actions in the menu, which are reachable,
 * and Escape clears a selection made by pointer.
 */
export const FileDiffSection = memo(function FileDiffSection({
  file,
  collapsed,
  onToggle,
  wrap,
  selection,
  onSelectionChange,
  onQuoteLines,
  onAskAboutFile,
  onDiscard,
}: FileDiffSectionProps) {
  const lines = useMemo(() => parseDiffLines(file.diff), [file.diff]);
  const Chevron = collapsed ? ChevronRight : ChevronDown;
  const selFrom = selection ? Math.min(selection.from, selection.to) : -1;
  const selTo = selection ? Math.max(selection.from, selection.to) : -1;

  const handleLineClick = (event: React.MouseEvent<HTMLDivElement>) => {
    const target = (event.target as HTMLElement).closest<HTMLElement>("[data-line-index]");
    const index = target ? Number(target.dataset.lineIndex) : Number.NaN;
    if (Number.isNaN(index)) return;
    if (event.shiftKey && selection) {
      onSelectionChange({ from: selection.from, to: index });
      return;
    }
    if (selection && selFrom === index && selTo === index) {
      onSelectionChange(null);
      return;
    }
    onSelectionChange({ from: index, to: index });
  };

  return (
    <section className="border-b last:border-b-0">
      <div className="sticky top-0 z-10 flex items-center gap-1.5 border-b bg-background/95 px-3 py-1.5 backdrop-blur">
        <button
          type="button"
          onClick={onToggle}
          aria-expanded={!collapsed}
          className="flex min-w-0 flex-1 cursor-pointer items-center gap-1.5 text-left"
        >
          <Chevron className="size-3 shrink-0 text-muted-foreground-faint" />
          <span className="shrink-0">{statusIcon(file.status)}</span>
          <FilePath path={file.path} className="font-mono text-xs" />
        </button>
        <span className="flex shrink-0 items-center gap-1.5 text-[11px] tabular-nums">
          {file.uncommitted && (
            <span
              className="inline-block size-1.5 rounded-full bg-warning"
              title="Not committed yet"
            />
          )}
          {file.insertions > 0 && <span className="text-success">+{file.insertions}</span>}
          {file.deletions > 0 && <span className="text-destructive">-{file.deletions}</span>}
        </span>
        <FileMenu
          path={file.path}
          onAskAboutFile={onAskAboutFile}
          {...(onDiscard ? { onDiscard } : {})}
        />
      </div>

      {!collapsed &&
        (hasRenderableDiff(lines) ? (
          <div
            onClick={handleLineClick}
            onKeyDown={(event) => {
              if (event.key === "Escape") onSelectionChange(null);
            }}
            className="overflow-x-auto font-mono text-[11.5px] leading-[1.55]"
          >
            {lines.map((line, index) =>
              line.kind === "meta" ? null : (
                // Index is the identity here: a diff line has no other one,
                // and the array is rebuilt whole whenever the diff changes.
                // biome-ignore lint/suspicious/noArrayIndexKey: see above
                <div key={index}>
                  <DiffRow
                    line={line}
                    index={index}
                    wrap={wrap}
                    selected={index >= selFrom && index <= selTo}
                  />
                  {/* Under the last selected line, not at the end of the file:
                      in a long diff the bottom of the patch is nowhere near
                      the lines you just picked. */}
                  {index === selTo && onQuoteLines && (
                    <SelectionBar
                      count={selTo - selFrom + 1}
                      onQuote={() => onQuoteLines(lines, selFrom, selTo)}
                      onClear={() => onSelectionChange(null)}
                    />
                  )}
                </div>
              ),
            )}
          </div>
        ) : (
          <p className="px-3 py-2 text-[11px] text-muted-foreground-faint">
            {diffNote(lines) ?? (file.status === "deleted" ? "File deleted." : "No textual diff.")}
          </p>
        ))}
    </section>
  );
});

function DiffRow({
  line,
  index,
  wrap,
  selected,
}: {
  line: DiffLine;
  index: number;
  wrap: boolean;
  selected: boolean;
}) {
  const number = line.newNo ?? line.oldNo;
  return (
    <div
      data-line-index={index}
      className={cn(
        "flex cursor-text",
        kindRowClass[line.kind],
        selected && "bg-primary/15",
        wrap ? "" : "w-max min-w-full",
      )}
    >
      <span
        className={cn(
          "w-9 shrink-0 select-none px-1.5 text-right text-[10px] text-muted-foreground-faint tabular-nums",
          selected && "bg-primary/25 text-primary",
        )}
      >
        {number ?? ""}
      </span>
      <span
        className={cn(
          "min-w-0 flex-1 pr-3",
          wrap ? "whitespace-pre-wrap break-all" : "whitespace-pre",
        )}
      >
        {line.text === "" ? " " : line.text}
      </span>
    </div>
  );
}

/**
 * What the selection is for. It sits in the flow under the last selected line
 * rather than floating: a floating bar in a 380px column covers the code it is
 * talking about.
 */
function SelectionBar({
  count,
  onQuote,
  onClear,
}: {
  count: number;
  onQuote: () => void;
  onClear: () => void;
}) {
  return (
    <div className="flex items-center gap-2 border-primary/30 border-y bg-primary/5 px-3 py-1.5 font-sans">
      <span className="text-[11px] text-muted-foreground tabular-nums">
        {count} {count === 1 ? "line" : "lines"}
      </span>
      <Button
        variant="ghost"
        size="xs"
        onClick={(event) => {
          event.stopPropagation();
          onQuote();
        }}
        className="ml-auto text-primary hover:bg-primary/10"
      >
        <MessageSquareQuote className="size-3" />
        Ask about this
      </Button>
      <Button
        variant="ghost"
        size="xs"
        onClick={(event) => {
          event.stopPropagation();
          onClear();
        }}
        className="text-muted-foreground-dim"
      >
        Clear
      </Button>
    </div>
  );
}

function FileMenu({
  path,
  onAskAboutFile,
  onDiscard,
}: {
  path: string;
  onAskAboutFile?: () => void;
  onDiscard?: () => void;
}) {
  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button
          variant="ghost"
          size="icon-xs"
          className="shrink-0 text-muted-foreground-faint hover:text-foreground"
          aria-label={`Actions for ${path}`}
        >
          <MoreHorizontal className="size-3.5" />
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" className="w-52">
        <DropdownMenuItem
          className="gap-2 text-xs"
          onClick={() => navigator.clipboard?.writeText(path)}
        >
          <Copy className="size-3.5 text-muted-foreground-dim" />
          Copy path
        </DropdownMenuItem>
        {onAskAboutFile && (
          <DropdownMenuItem className="gap-2 text-xs" onClick={onAskAboutFile}>
            <MessageSquareQuote className="size-3.5 text-muted-foreground-dim" />
            Ask about this file
          </DropdownMenuItem>
        )}
        {onDiscard && (
          <DropdownMenuItem
            className="gap-2 text-destructive text-xs focus:text-destructive"
            onClick={onDiscard}
          >
            Discard changes
          </DropdownMenuItem>
        )}
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
