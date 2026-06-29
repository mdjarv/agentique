import { Activity } from "lucide-react";
import { Button } from "~/components/ui/button";
import { Popover, PopoverContent, PopoverTrigger } from "~/components/ui/popover";
import { type BrainCounts, EVIDENCE_VALUES, VOLATILITY_VALUES } from "~/lib/brain-api";
import { useBrainStore } from "~/stores/brain-store";

const CONFIDENCE_TIERS = ["extracted", "inferred", "ambiguous"] as const;

// Short display labels for the controlled-vocabulary keys in the distributions.
const LABEL: Record<string, string> = {
  user_stated: "stated",
  code_verified: "code-verified",
  corroborated: "corroborated",
  inferred: "inferred",
  observed_once: "seen once",
  evergreen: "evergreen",
  slow: "slow",
  ephemeral: "ephemeral",
  extracted: "extracted",
  ambiguous: "ambiguous",
};

// BrainHealth is the Band-3 E2 report: a popover answering "what state is the brain in?" —
// the capture backlog, archived/superseded counts, the evidence/volatility/confidence
// spread, and the review backlog (brain-ui-spec.md F6). Read-only; the counts come from the
// status endpoint and refresh on brain.updated (debounced in the store). The component is
// structured so a future Band-2 Curator "recent churn" list can slot in below the strip.
export function BrainHealth() {
  // counts is a stored object reference (set as a whole on load/refresh), so selecting it
  // directly is stable — no fresh object from the selector (CLAUDE.md stable-selector rule).
  const counts = useBrainStore((s) => s.counts);

  return (
    <Popover>
      <PopoverTrigger asChild>
        <Button size="sm" variant="outline" title="Brain health — the Band-1 pipeline at a glance">
          <Activity className="size-4" /> Health
        </Button>
      </PopoverTrigger>
      <PopoverContent align="end" className="w-80">
        {counts ? (
          <HealthBody counts={counts} />
        ) : (
          <div className="text-xs text-muted-foreground">Loading…</div>
        )}
      </PopoverContent>
    </Popover>
  );
}

function HealthBody({ counts }: { counts: BrainCounts }) {
  return (
    <div className="space-y-3 text-xs">
      <div className="text-sm font-medium">Brain health</div>

      {/* Pipeline summary — the load-bearing numbers. */}
      <div className="grid grid-cols-2 gap-x-4 gap-y-1.5">
        <Stat label="Total facts" value={counts.total} />
        <Stat label="Captures pending" value={counts.bySource.capture ?? 0} accent />
        <Stat label="Archived" value={counts.byLifecycle.archived ?? 0} />
        <Stat label="Superseded" value={counts.byLifecycle.superseded ?? 0} />
        <Stat label="Review queue" value={counts.reviewQueue} accent />
        <Stat label="Corroborations" value={counts.corroboratedTotal} />
      </div>

      <Dist title="Evidence" map={counts.byEvidence} order={EVIDENCE_VALUES} />
      <Dist title="Volatility" map={counts.byVolatility} order={VOLATILITY_VALUES} />
      <Dist title="Confidence" map={counts.byConfidenceTier} order={CONFIDENCE_TIERS} />
    </div>
  );
}

function Stat({ label, value, accent }: { label: string; value: number; accent?: boolean }) {
  return (
    <div className="flex items-baseline justify-between gap-2">
      <span className="text-muted-foreground">{label}</span>
      <span className={`tabular-nums ${accent && value > 0 ? "font-medium text-amber-600" : ""}`}>
        {value}
      </span>
    </div>
  );
}

function Dist({
  title,
  map,
  order,
}: {
  title: string;
  map: Record<string, number>;
  order: readonly string[];
}) {
  return (
    <div className="space-y-1">
      <div className="text-[11px] font-medium uppercase tracking-wide text-muted-foreground">
        {title}
      </div>
      <div className="flex flex-wrap gap-1">
        {order.map((k) => (
          <span
            key={k}
            className="rounded bg-muted/60 px-1.5 py-0.5 text-muted-foreground"
            title={`${map[k] ?? 0} ${LABEL[k] ?? k}`}
          >
            {LABEL[k] ?? k}{" "}
            <span className="font-medium text-foreground/80 tabular-nums">{map[k] ?? 0}</span>
          </span>
        ))}
      </div>
    </div>
  );
}
