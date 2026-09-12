/**
 * One card in the deck's "Needs you" band.
 *
 * Four reasons land here and they are not equals: an approval and a question
 * hold a process, a proposal holds an uncontained verb the assistant will not
 * perform, an unread completion holds only the operator's attention. So the
 * three that are somebody's to answer keep the amber card and its inline
 * actions, while an unread one is a quiet card you open — same band, because
 * all of them are things the operator has to close out, different weight,
 * because only two of them are costing wall-clock.
 */
import { Check, CircleHelp, TriangleAlert } from "lucide-react";
import { useState } from "react";
import { toast } from "sonner";
import { useWebSocket } from "~/hooks/useWebSocket";
import { proposalWords } from "~/lib/assistant/proposal-words";
import { decide } from "~/lib/assistant/rpc";
import { resolveApproval } from "~/lib/session/actions";
import { REST_GLYPH } from "~/lib/session/rest-state";
import { cn, getErrorMessage } from "~/lib/utils";
import { useAssistantStore } from "~/stores/assistant-store";
import type { DeckKind, DeckRow } from "./use-deck-rows";

const KIND_GLYPH: Record<DeckKind, typeof Check> = {
  approval: TriangleAlert,
  question: CircleHelp,
  // The same triangle the approval row wears, because it means the same thing
  // here: someone is waiting on you. One mark, one meaning, across surfaces —
  // a proposal is not a failure and not a question, it is a yes nobody has
  // given yet.
  proposal: TriangleAlert,
  unread: Check,
};

/** Blocked on a human — the amber monopoly, same as the sidebar's. */
function isBlocked(kind: DeckKind): boolean {
  return kind === "approval" || kind === "question" || kind === "proposal";
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
  const [deciding, setDeciding] = useState(false);
  const blocked = isBlocked(row.kind);
  const proposal = row.kind === "proposal";
  const Glyph = KIND_GLYPH[row.kind];

  const resolve = (allow: boolean) => {
    if (!row.approvalId) return;
    resolveApproval(ws, row.sessionId, row.approvalId, allow).catch((err) =>
      toast.error(getErrorMessage(err, "Failed to resolve approval")),
    );
  };

  // Accept re-checks the facts server-side and may answer `stale` having done
  // nothing, so the answer goes into the store: the row leaves this band and
  // the thread's card — which has the room to say what happened — carries the
  // outcome. The deck is triage, not a place to read a result.
  const settle = (accept: boolean) => {
    if (!row.proposalId || deciding) return;
    setDeciding(true);
    decide(ws, row.proposalId, accept)
      .then((decided) => useAssistantStore.getState().applyProposal(decided))
      .catch((err) => toast.error(getErrorMessage(err, "Failed to decide the proposal")))
      .finally(() => setDeciding(false));
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
            blocked ? "text-orange" : "text-success",
            // A pulse means live activity. An approval and a question hold a
            // process that is idling on an answer; a proposal is a row in a
            // table and nothing is waiting on the clock, so it does not move.
            (row.kind === "approval" || row.kind === "question") &&
              "animate-pulse motion-reduce:animate-none",
          )}
        />
        <span className="min-w-0 flex-1 truncate text-[13.5px] font-semibold text-foreground-bright">
          {proposal ? proposalWords(row.verb) : row.name || "Untitled"}
        </span>
        <Identity row={row} />
      </div>
      {/* A proposal's target is the line under the verb, because the verb is
          what the reader is deciding and the session is what it is about. */}
      {proposal && row.name && (
        <div className="mb-1 truncate text-[11px] text-muted-foreground">{row.name}</div>
      )}
      {row.summary &&
        (proposal ? (
          // The assistant's own reason — model-written text, quoted rather than
          // printed as one of the server's facts.
          <blockquote className="mb-2 border-l-2 border-agent/40 pl-2 text-[11px] italic text-foreground/80 line-clamp-2">
            {row.summary}
          </blockquote>
        ) : (
          <div className="mb-2 truncate font-mono text-[11px] text-orange">{row.summary}</div>
        ))}
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
        {proposal && (
          <>
            <CardAction
              label="Accept"
              disabled={deciding}
              onAction={() => settle(true)}
              className="bg-success text-primary-foreground hover:opacity-90"
            />
            <CardAction
              label="Decline"
              disabled={deciding}
              onAction={() => settle(false)}
              className="text-destructive hover:bg-destructive/10"
            />
          </>
        )}
        {/* Nothing to open when the proposal is about a channel, or about a
            session on a machine this client has no project row for. */}
        {(!proposal || (row.sessionId && row.projectSlug)) && (
          <CardAction
            label={row.kind === "unread" ? "Read ›" : "open ›"}
            onAction={onOpen}
            className="text-muted-foreground hover:bg-secondary hover:text-foreground"
          />
        )}
      </div>
    </div>
  );
}

function CardAction({
  label,
  onAction,
  className,
  disabled,
}: {
  label: string;
  onAction: () => void;
  className: string;
  disabled?: boolean;
}) {
  return (
    <button
      type="button"
      onClick={onAction}
      disabled={disabled}
      className={cn(
        "cursor-pointer rounded-md px-2.5 py-1.5 text-[11px] font-semibold disabled:cursor-default disabled:opacity-60",
        className,
      )}
    >
      {label}
    </button>
  );
}
