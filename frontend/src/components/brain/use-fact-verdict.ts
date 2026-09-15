import { useCallback, useState } from "react";
import { toast } from "sonner";
import { confirmMemory, flagMemory } from "~/lib/brain-api";

/** Where the operator's verdict on one fact has got to. */
export type FactVerdict = "idle" | "confirming" | "flagging" | "helpful" | "flagged";

/**
 * The human side of memory's outcome signal for one fact: Helpful confirms it,
 * Outdated flags it for review.
 *
 * One hook for every surface that offers the pair — the old recall card in a
 * session transcript and the recall row in the assistant's thread — so the two
 * say the same thing when pressed. A verdict is given once: after it lands, the
 * pair is a result rather than two buttons, because confirming a fact twice
 * from one screen is a double count, not a stronger yes.
 */
export function useFactVerdict(id: string | undefined) {
  const [status, setStatus] = useState<FactVerdict>("idle");
  const busy = status === "confirming" || status === "flagging";
  const settled = status === "helpful" || status === "flagged";

  const act = useCallback(
    async (kind: "helpful" | "flag") => {
      if (busy || settled || !id) return;
      setStatus(kind === "helpful" ? "confirming" : "flagging");
      try {
        if (kind === "helpful") {
          await confirmMemory(id);
          setStatus("helpful");
          toast.success("Marked helpful — confirmed in memory");
        } else {
          await flagMemory(id);
          setStatus("flagged");
          toast.success("Flagged as outdated for review");
        }
      } catch (err) {
        setStatus("idle");
        toast.error(err instanceof Error ? err.message : "Failed to update memory");
      }
    },
    [busy, settled, id],
  );

  return { status, busy, settled, act };
}
