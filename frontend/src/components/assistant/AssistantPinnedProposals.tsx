/**
 * What is waiting for a yes, pinned above the composer while its card is out of
 * sight.
 *
 * The card lives in the timeline, at the moment the assistant raised it, so it
 * scrolls away like everything else — and a decision owed is the one thing on
 * the page that must not. This is the session view's rule for an approval
 * (pinned above the composer regardless of scroll), with one difference: it
 * leads to the card rather than deciding in place. The card carries the facts
 * and the assistant's reason, and a yes to a merge given from a line that shows
 * neither is the button nobody can judge that the card exists to prevent.
 *
 * It steps aside for proposals whose card is on screen, which the page reports:
 * the same card twice, one above the other, reads as two things to decide.
 */
import { TriangleAlert } from "lucide-react";
import { useSessionLabel } from "~/components/assistant/use-session-label";
import { proposalWords } from "~/lib/assistant/proposal-words";
import type { AssistantProposal } from "~/lib/assistant/wire";

interface AssistantPinnedProposalsProps {
  /** Open proposals whose card is not on screen, oldest first. */
  proposals: AssistantProposal[];
  /** Brings a proposal's card into view. */
  onShow: (id: string) => void;
}

export function AssistantPinnedProposals({ proposals, onShow }: AssistantPinnedProposalsProps) {
  const [first] = proposals;
  const target = useSessionLabel(first?.sessionId, first?.sessionName);
  if (!first?.id) return null;
  const id = first.id;
  const more = proposals.length - 1;
  const subject = target ?? (typeof first.args?.channel === "string" ? first.args.channel : "");

  return (
    <section
      aria-label="Waiting on you"
      className="mx-3 mb-2 flex shrink-0 items-center gap-2 rounded-lg border border-orange/40 bg-orange/5 px-3 py-1.5 text-xs max-md:mx-0 max-md:mb-0 max-md:rounded-none max-md:border-x-0 max-md:border-b-0"
    >
      <TriangleAlert className="size-3 shrink-0 text-orange" aria-hidden />
      <p className="min-w-0 flex-1 truncate">
        <span className="font-semibold text-foreground-bright">{proposalWords(first.verb)}</span>
        {subject && <span className="text-muted-foreground"> · {subject}</span>}
        {first.projectName && <span className="text-muted-foreground"> · {first.projectName}</span>}
      </p>
      {more > 0 && <span className="shrink-0 text-muted-foreground">+{more} more</span>}
      <button
        type="button"
        onClick={() => onShow(id)}
        className="shrink-0 cursor-pointer rounded-md px-2 py-1 font-semibold text-orange hover:bg-orange/10 max-md:py-2"
      >
        Review
      </button>
    </section>
  );
}
