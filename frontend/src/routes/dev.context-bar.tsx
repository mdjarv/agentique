import { createFileRoute } from "@tanstack/react-router";
import { ContextBar } from "~/components/chat/ContextBar";
import { ContextEdge } from "~/components/chat/composer/ContextEdge";
import { contextTier } from "~/lib/session/context-tier";

export const Route = createFileRoute("/dev/context-bar")({
  component: DevContextBar,
});

/**
 * The context reading at every tier, on both of its surfaces.
 *
 * The point of the gallery is that they are the same table: `ContextBar` is the
 * desktop's row and `ContextEdge` is the phone's composer edge, and both read
 * `lib/session/context-tier`. A tier that looks different here is a tier that
 * would look different in the app.
 */
const WINDOW = 1_000_000;
const SAMPLES = [14, 59, 60, 79, 80, 94, 95, 100];

function usageAt(pct: number) {
  return {
    contextWindow: WINDOW,
    inputTokens: 0,
    outputTokens: 0,
    usedTokens: Math.round((pct / 100) * WINDOW),
  };
}

function DevContextBar() {
  return (
    <div className="p-8 space-y-8 max-w-3xl">
      <div className="space-y-1">
        <h1 className="text-lg font-semibold">Context reading — every tier, both surfaces</h1>
        <p className="text-sm text-muted-foreground">
          Quiet below 80%: a colour and nothing else. At 80 the edge grows a band and gets its words
          back, because a level that decides what you should type next has earned them.
        </p>
      </div>

      {SAMPLES.map((pct) => (
        <div key={pct} className="space-y-2">
          <span className="text-xs font-mono text-muted-foreground">
            {pct}% — {contextTier(pct).tier}
          </span>
          <div className="border rounded-md bg-background overflow-hidden">
            <ContextBar usage={usageAt(pct)} />
          </div>
          <div className="w-[393px] border rounded-md bg-background overflow-hidden">
            <div className="h-10 bg-muted/20" />
            <ContextEdge usage={usageAt(pct)} />
            <div className="px-3 py-3 text-sm text-muted-foreground">Send a message...</div>
          </div>
        </div>
      ))}

      <div className="space-y-2">
        <span className="text-xs font-mono text-muted-foreground">compacting</span>
        <div className="w-[393px] border rounded-md bg-background overflow-hidden">
          <div className="h-10 bg-muted/20" />
          <ContextEdge compacting />
          <div className="px-3 py-3 text-sm text-muted-foreground">Compacting context...</div>
        </div>
      </div>
    </div>
  );
}
