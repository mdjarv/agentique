/**
 * One card in the deck's "Needs you" band.
 *
 * Three reasons land here and they are not equals: an approval and a question
 * hold a process, an unread completion holds only the operator's attention. So
 * the two blocked kinds keep the amber card and its inline resolve, while an
 * unread one is a quiet card you open — same band, because both are things the
 * operator has to close out, different weight, because only one of them is
 * costing wall-clock.
 */
import { Check, CircleHelp, TriangleAlert } from "lucide-react";
import { toast } from "sonner";
import { useWebSocket } from "~/hooks/useWebSocket";
import { resolveApproval } from "~/lib/session/actions";
import { REST_GLYPH } from "~/lib/session/rest-state";
import { cn, getErrorMessage } from "~/lib/utils";
import type { DeckKind, DeckRow } from "./use-deck-rows";

const KIND_GLYPH: Record<DeckKind, typeof Check> = {
  approval: TriangleAlert,
  question: CircleHelp,
  unread: Check,
};

/** Blocked on a human — the amber monopoly, same as the sidebar's. */
function isBlocked(kind: DeckKind): boolean {
  return kind === "approval" || kind === "question";
}

/**
 * The identity line: project hue and, at rest, the mark for how the run ended.
 * The mark is the deck's half of the rail's rule — a session someone killed or
 * whose CLI agentique reclaimed says so, instead of going quiet.
 */
function Identity({ row }: { row: DeckRow }) {
  const Mark = row.restToken ? REST_GLYPH[row.restToken] : null;
  return (
    <span className="flex shrink-0 items-center gap-1 font-mono text-[10px]">
      <span style={{ color: row.projectColorFg }}>{row.projectLabel}</span>
      {Mark && (
        <span className="flex items-center gap-0.5 text-muted-foreground-faint">
          <Mark className="size-2.5 shrink-0" />
          {row.restToken}
        </span>
      )}
    </span>
  );
}

export function AttentionCard({ row, onOpen }: { row: DeckRow; onOpen: () => void }) {
  const ws = useWebSocket();
  const blocked = isBlocked(row.kind);
  const Glyph = KIND_GLYPH[row.kind];

  const resolve = (allow: boolean) => {
    if (!row.approvalId) return;
    resolveApproval(ws, row.sessionId, row.approvalId, allow).catch((err) =>
      toast.error(getErrorMessage(err, "Failed to resolve approval")),
    );
  };

  return (
    <div
      className={cn(
        "max-w-md flex-1 rounded-xl border p-3.5",
        blocked ? "border-orange/40 bg-orange/5" : "border-border/60 bg-card",
      )}
    >
      <div className="mb-1 flex items-center gap-2">
        <Glyph
          className={cn(
            "size-3 shrink-0",
            blocked ? "animate-pulse text-orange motion-reduce:animate-none" : "text-success",
          )}
        />
        <span className="min-w-0 flex-1 truncate text-[13.5px] font-semibold text-foreground-bright">
          {row.name || "Untitled"}
        </span>
        <Identity row={row} />
      </div>
      {row.summary && (
        <div className="mb-2 truncate font-mono text-[11px] text-orange">{row.summary}</div>
      )}
      <div className="flex gap-1.5">
        {row.kind === "approval" && (
          <>
            <CardAction
              label="Allow"
              onAction={() => resolve(true)}
              className="bg-success text-primary-foreground hover:opacity-90"
            />
            <CardAction
              label="Deny"
              onAction={() => resolve(false)}
              className="text-destructive hover:bg-destructive/10"
            />
          </>
        )}
        {row.kind === "question" && (
          <CardAction
            label="Answer…"
            onAction={onOpen}
            className="text-foreground hover:bg-secondary"
          />
        )}
        <CardAction
          label={row.kind === "unread" ? "Read ›" : "open ›"}
          onAction={onOpen}
          className="text-muted-foreground hover:bg-secondary hover:text-foreground"
        />
      </div>
    </div>
  );
}

function CardAction({
  label,
  onAction,
  className,
}: {
  label: string;
  onAction: () => void;
  className: string;
}) {
  return (
    <button
      type="button"
      onClick={onAction}
      className={cn("cursor-pointer rounded-md px-2.5 py-1.5 text-[11px] font-semibold", className)}
    >
      {label}
    </button>
  );
}
