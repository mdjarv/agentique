/**
 * The landing page's triage bands — needs-you (with inline resolve) and live
 * now. Either renders only when non-empty; when both are empty the deck
 * reduces to a single all-quiet line and the wire below owns the page.
 *
 * There used to be a third band for unread completions, which was a distinction
 * without a difference: an approval, an open question and a finished-but-unread
 * turn are all one thing to the operator — sessions waiting on them. They share
 * a band now, ordered so the two that hold a process come first.
 *
 * Repo drift deliberately does NOT live here: the sidebar's sync dock owns it,
 * and two lists of the same four repos is worse than either alone.
 */
import { useNavigate } from "@tanstack/react-router";
import { cn, sessionShortId } from "~/lib/utils";
import { AttentionCard } from "./AttentionCard";
import { type LiveRow, useDeckRows } from "./use-deck-rows";

function SectionLabel({ children }: { children: React.ReactNode }) {
  return (
    <div className="mb-2 text-[10px] font-semibold uppercase tracking-wider text-muted-foreground">
      {children}
    </div>
  );
}

function useOpenSession() {
  const navigate = useNavigate();
  return (projectSlug: string, sessionId: string) => {
    if (!projectSlug) return;
    navigate({
      to: "/project/$projectSlug/session/$sessionShortId",
      params: { projectSlug, sessionShortId: sessionShortId(sessionId) },
    });
  };
}

function LiveCard({ row, onOpen }: { row: LiveRow; onOpen: () => void }) {
  return (
    <button
      type="button"
      onClick={onOpen}
      className="max-w-md flex-1 cursor-pointer rounded-xl border border-border/60 bg-card p-3 text-left transition-colors hover:border-border"
    >
      <div className="flex items-center gap-2">
        <span className="size-[7px] shrink-0 rounded-full bg-teal" />
        <span className="min-w-0 flex-1 truncate text-[13px] font-semibold text-foreground-bright">
          {row.name || "Untitled"}
        </span>
        <span className="shrink-0 font-mono text-[10px]" style={{ color: row.projectColorFg }}>
          {row.projectLabel}
        </span>
      </div>
      {row.pulse && (
        <div className="mt-1 truncate font-mono text-[10.5px] text-teal">{row.pulse}</div>
      )}
      {row.todo && (
        <div className="mt-2 h-[3px] overflow-hidden rounded-full bg-border/60">
          <div
            className="h-full rounded-full bg-primary"
            style={{ width: `${Math.round((row.todo.done / row.todo.total) * 100)}%` }}
          />
        </div>
      )}
    </button>
  );
}

export function CommandDeck() {
  const { needs, live } = useDeckRows();
  const open = useOpenSession();

  if (needs.length === 0 && live.length === 0) {
    return (
      <div className="flex items-center gap-2.5 text-[12.5px] text-muted-foreground">
        <span className="size-2 rounded-full bg-success opacity-70" />
        All quiet — nothing needs you.
      </div>
    );
  }

  return (
    <div className={cn("flex flex-col gap-4")}>
      {needs.length > 0 && (
        <div>
          <SectionLabel>
            Needs you <span className="font-mono text-muted-foreground-faint">{needs.length}</span>
          </SectionLabel>
          <div className="flex flex-wrap gap-3">
            {needs.map((row) => (
              <AttentionCard
                key={row.sessionId}
                row={row}
                onOpen={() => open(row.projectSlug, row.sessionId)}
              />
            ))}
          </div>
        </div>
      )}

      {live.length > 0 && (
        <div>
          <SectionLabel>
            Live now <span className="font-mono text-muted-foreground-faint">{live.length}</span>
          </SectionLabel>
          <div className="flex flex-wrap gap-3">
            {live.map((row) => (
              <LiveCard
                key={row.sessionId}
                row={row}
                onOpen={() => open(row.projectSlug, row.sessionId)}
              />
            ))}
          </div>
        </div>
      )}
    </div>
  );
}
