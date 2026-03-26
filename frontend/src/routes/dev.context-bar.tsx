import { createFileRoute } from "@tanstack/react-router";
import { AlertTriangle } from "lucide-react";
import { Progress } from "~/components/ui/progress";
import { cn } from "~/lib/utils";

export const Route = createFileRoute("/dev/context-bar")({
  component: DevContextBar,
});

const orange = {
  bar: "h-1.5 flex-1 bg-orange-500/15 [&>[data-slot=progress-indicator]]:bg-orange-500",
  text: "text-orange-400",
};
const red = {
  bar: "h-1.5 flex-1 bg-red-500/15 [&>[data-slot=progress-indicator]]:bg-red-500",
  text: "text-red-500",
};

function BarRow({
  pct,
  label,
  tokens,
  tier,
}: { pct: number; label: string; tokens: string; tier: typeof orange }) {
  return (
    <div className="flex items-center gap-2 px-4 py-1">
      <span className={cn("inline-flex items-center gap-1 text-[11px] shrink-0", tier.text)}>
        <AlertTriangle className="size-3" />
        {label}
      </span>
      <Progress value={pct} className={tier.bar} />
      <span className={cn("text-[11px] tabular-nums shrink-0", tier.text)}>
        {pct}%<span className="text-muted-foreground/60 ml-1">{tokens}</span>
      </span>
    </div>
  );
}

const pairs = [
  { w: "Near Limit", i: "At Limit" },
  { w: "Running Low", i: "Almost Full" },
  { w: "Context Low", i: "Context Full" },
  { w: "Filling Up", i: "Nearly Full" },
  { w: "High Usage", i: "Critical" },
];

function DevContextBar() {
  return (
    <div className="p-8 space-y-10">
      <h1 className="text-lg font-semibold">Context Bar — wording pairs (80% / 96%)</h1>
      {pairs.map((p) => (
        <div key={p.w} className="space-y-2">
          <span className="text-sm text-muted-foreground font-medium">
            "{p.w}" / "{p.i}"
          </span>
          <div className="border rounded-md bg-background">
            <BarRow pct={80} label={p.w} tokens="160k/200k" tier={orange} />
          </div>
          <div className="border rounded-md bg-background">
            <BarRow pct={96} label={p.i} tokens="192k/200k" tier={red} />
          </div>
        </div>
      ))}
    </div>
  );
}
