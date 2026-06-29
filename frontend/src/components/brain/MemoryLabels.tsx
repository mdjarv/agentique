import { Badge } from "~/components/ui/badge";
import type { Memory } from "~/lib/brain-api";
import { evidenceChip, isCapture, lifecycleBadge, volatilityChip } from "~/lib/brain-labels";

// MemoryLabels renders a memory's tier vocabulary inline (brain-ui-spec.md F1): a
// capture / archived / superseded badge, evidence + volatility chips, and a corroboration
// count. Pure/presentational and read-only — reused by the list rows (BrainPage) and the
// review surface (MemoryReview) so the two stay consistent. It emits a fragment of inline
// items meant to drop into an existing flex-wrap row; it renders nothing for an ordinary
// active, non-capture, default-labelled fact (so quiet rows stay quiet).
export function MemoryLabels({ memory }: { memory: Memory }) {
  const capture = isCapture(memory);
  const lifecycle = lifecycleBadge(memory);
  const evidence = evidenceChip(memory.evidence);
  const volatility = volatilityChip(memory.volatility);
  const corroborations = memory.corroborations ?? 0;

  if (!capture && !lifecycle && !evidence && !volatility && corroborations === 0) return null;

  return (
    <>
      {capture && (
        <Badge variant="capture" title="Raw capture — never injected, awaiting promotion">
          capture
        </Badge>
      )}
      {lifecycle && (
        <Badge variant={lifecycle.variant} title={lifecycle.title}>
          {lifecycle.label}
        </Badge>
      )}
      {evidence && (
        <Badge variant="evidence" title={evidence.title}>
          {evidence.label}
        </Badge>
      )}
      {volatility && (
        <Badge variant="volatility" title={volatility.title}>
          {volatility.label}
        </Badge>
      )}
      {corroborations > 0 && (
        <span
          className="text-[10px] text-muted-foreground tabular-nums"
          title={`Corroborated ${corroborations}× — independent re-observations`}
        >
          ×{corroborations}
        </span>
      )}
    </>
  );
}
