/**
 * The cards between the strip and the conversation — what is waiting for a yes.
 *
 * Above the conversation, because a proposal is not a line of news: the strip
 * reports what happened and scrolls away, where this is the one thing on the
 * page that is the reader's to act on. Below the strip rather than above it so
 * the page reads top-down as "here is what happened, here is what I would like
 * to do about it".
 *
 * It renders the open rows PLUS anything decided while this page has been on
 * screen. A card that vanished the moment it was pressed would take its own
 * answer with it — and the answer is the interesting half, since accepting
 * re-checks the facts and can come back `stale` having done nothing. The
 * decided set is local to the page for exactly that reason: it is "what I have
 * pressed", not state anybody else shares, and leaving the page clears it.
 */
import { useMemo, useState } from "react";
import { ProposalCard } from "~/components/assistant/ProposalCard";
import { isOpenProposal } from "~/lib/assistant/wire";
import { selectAssistantProposals, useAssistantStore } from "~/stores/assistant-store";

const NOTHING_PRESSED: ReadonlySet<string> = new Set();

export function AssistantProposals() {
  const proposals = useAssistantStore(selectAssistantProposals);
  const [pressed, setPressed] = useState<ReadonlySet<string>>(NOTHING_PRESSED);

  // Memoised in the COMPONENT, never in the selector: a `.filter()` inside a
  // Zustand selector mints a new array on every call and re-renders forever.
  const cards = useMemo(
    () => proposals.filter((row) => isOpenProposal(row.status) || (row.id && pressed.has(row.id))),
    [proposals, pressed],
  );

  if (cards.length === 0) return null;

  const openCount = cards.filter((row) => isOpenProposal(row.status)).length;

  return (
    <section aria-label="Proposals" className="shrink-0 border-b px-3 py-2">
      <div className="mb-1.5 flex items-baseline gap-2">
        <h2 className="text-[10px] uppercase tracking-wide text-muted-foreground">Proposals</h2>
        {openCount > 0 && (
          <span className="text-[10px] text-muted-foreground-faint">
            {openCount} waiting on you
          </span>
        )}
      </div>
      <div className="flex flex-col gap-2 overflow-y-auto max-h-72">
        {cards.map((row) => (
          <ProposalCard
            key={row.id}
            proposal={row}
            onDecided={(id) => setPressed((held) => new Set(held).add(id))}
          />
        ))}
      </div>
    </section>
  );
}
