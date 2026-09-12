/**
 * One proposal, as a card somebody presses.
 *
 * The assistant never performs an uncontained verb: asking for one writes a row
 * and this is where the yes lives. So the card has to carry everything a
 * decision needs from somebody who was not in the conversation — what would
 * happen, to which session in which project, the server facts it was judged on,
 * and the assistant's own reason — and then get out of the way.
 *
 * Three kinds of text, three treatments, and the difference is who wrote them.
 * The **verb** is this client's words from a closed table. The **evidence** is
 * the server's own reading of git and the runtime, printed as facts. The
 * **rationale** is the assistant's sentence, which means it is model-written
 * text about untrusted repository content: it is quoted and attributed, never
 * set as the server's claim, on the same rule the strip applies to a report.
 *
 * Accepting re-checks the facts server-side before anything happens, so what
 * comes back is the interesting part: `stale` means the branch moved under the
 * card and NOTHING was done, `failed` means it was tried. Both are rendered
 * here, in place, rather than toasted and forgotten — the reader pressed a
 * button and is owed the answer where they pressed it.
 */
import { Handshake, Loader2, TriangleAlert } from "lucide-react";
import { useState } from "react";
import { toast } from "sonner";
import { useSessionLabel } from "~/components/assistant/use-session-label";
import { useWebSocket } from "~/hooks/useWebSocket";
import {
  proposalFacts,
  proposalIsDestructive,
  proposalOutcomeWords,
  proposalWentWrong,
  proposalWords,
} from "~/lib/assistant/proposal-words";
import { decide as decideRpc } from "~/lib/assistant/rpc";
import { type AssistantProposal, isOpenProposal } from "~/lib/assistant/wire";
import { cn, getErrorMessage } from "~/lib/utils";
import { useAssistantStore } from "~/stores/assistant-store";

interface ProposalCardProps {
  proposal: AssistantProposal;
  /**
   * Called with the proposal's id once a decision has landed. The thread uses
   * it to keep the card on screen after it has left the open list — the answer
   * to a press has to be readable where the press happened.
   */
  onDecided?: (id: string) => void;
}

export function ProposalCard({ proposal, onDecided }: ProposalCardProps) {
  const ws = useWebSocket();
  const [pending, setPending] = useState<"accept" | "decline" | null>(null);
  const open = isOpenProposal(proposal.status);
  const facts = proposalFacts(proposal.evidence);
  const target = useSessionLabel(proposal.sessionId, proposal.sessionName);
  const channel = typeof proposal.args?.channel === "string" ? proposal.args.channel : undefined;

  const send = async (accept: boolean) => {
    const id = proposal.id;
    if (!id || pending) return;
    setPending(accept ? "accept" : "decline");
    try {
      // The answer is applied straight to the store rather than waited for on
      // the push: the row is the same either way, and a card that only becomes
      // its outcome when a broadcast arrives looks broken on a slow socket.
      const decided = await decideRpc(ws, id, accept);
      useAssistantStore.getState().applyProposal(decided);
      onDecided?.(id);
    } catch (err) {
      toast.error(getErrorMessage(err, "The assistant could not record that"));
    } finally {
      setPending(null);
    }
  };

  const Glyph = open ? TriangleAlert : Handshake;
  const wentWrong = !open && proposalWentWrong(proposal.status);

  return (
    <article
      aria-label={proposalWords(proposal.verb)}
      className={cn(
        "rounded-xl border p-3",
        open ? "border-orange/40 bg-orange/5" : "border-border/60 bg-card",
      )}
    >
      <div className="flex items-center gap-2">
        <Glyph
          className={cn("size-3 shrink-0", open ? "text-orange" : "text-muted-foreground")}
          aria-hidden
        />
        <h3 className="min-w-0 flex-1 truncate text-[13px] font-semibold text-foreground-bright">
          {proposalWords(proposal.verb)}
        </h3>
        {open && proposalIsDestructive(proposal.verb) && (
          <span className="shrink-0 text-[10px] uppercase tracking-wide text-destructive">
            irreversible
          </span>
        )}
      </div>

      {/* The target, always in its project: a session name is generated from a
          first prompt and blurs into every other one, where the project is the
          word the reader holds in their head. */}
      <p className="mt-0.5 truncate text-[11px] text-muted-foreground">
        {target ?? channel ?? "this machine"}
        {proposal.projectName ? ` · ${proposal.projectName}` : ""}
      </p>

      {facts.length > 0 && (
        <dl className="mt-2 flex flex-wrap gap-x-3 gap-y-0.5 font-mono text-[10.5px]">
          {facts.map((fact) => (
            <div key={fact.key} className="flex gap-1">
              <dt className="text-muted-foreground-faint">{fact.label}</dt>
              <dd className="text-foreground/90">{fact.value}</dd>
            </div>
          ))}
        </dl>
      )}

      {proposal.rationale && (
        <blockquote className="mt-2 border-l-2 border-agent/40 pl-2 text-[11.5px] italic leading-5 text-foreground/80">
          {proposal.rationale}
        </blockquote>
      )}

      {open ? (
        <div className="mt-2.5 flex gap-1.5">
          <CardAction
            label="Accept"
            busy={pending === "accept"}
            disabled={pending !== null}
            onAction={() => void send(true)}
            className="bg-success text-primary-foreground hover:opacity-90"
          />
          <CardAction
            label="Decline"
            busy={pending === "decline"}
            disabled={pending !== null}
            onAction={() => void send(false)}
            className="text-destructive hover:bg-destructive/10"
          />
        </div>
      ) : (
        <p
          className={cn(
            "mt-2 text-[11.5px]",
            wentWrong ? "text-destructive" : "text-muted-foreground",
          )}
        >
          {proposalOutcomeWords(proposal.status)}
          {proposal.outcome ? ` — ${proposal.outcome}` : ""}
        </p>
      )}
    </article>
  );
}

function CardAction({
  label,
  busy,
  disabled,
  onAction,
  className,
}: {
  label: string;
  busy: boolean;
  disabled: boolean;
  onAction: () => void;
  className: string;
}) {
  return (
    <button
      type="button"
      onClick={onAction}
      disabled={disabled}
      className={cn(
        "flex cursor-pointer items-center gap-1 rounded-md px-2.5 py-1.5 text-[11px] font-semibold disabled:cursor-default disabled:opacity-60",
        className,
      )}
    >
      {busy && <Loader2 className="size-3 animate-spin motion-reduce:animate-none" aria-hidden />}
      {label}
    </button>
  );
}
