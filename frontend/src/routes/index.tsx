import { createFileRoute } from "@tanstack/react-router";
import { Activity } from "lucide-react";
import { CommandDeck } from "~/components/home/CommandDeck";
import { TheWire } from "~/components/home/TheWire";
import { PageHeader } from "~/components/layout/PageHeader";

export const Route = createFileRoute("/")({
  component: HomePage,
});

/**
 * The landing page: the command deck (needs-you / live / to-review, each
 * band only when non-empty) over the wire, the ambient event river. When
 * nothing needs the operator, the deck collapses to one quiet line and the
 * page is just the river.
 */
function HomePage() {
  return (
    <div className="flex h-full flex-col">
      <PageHeader>
        <Activity className="size-4 text-muted-foreground" />
        <span className="font-semibold">Overview</span>
      </PageHeader>
      <div className="min-h-0 flex-1 overflow-y-auto">
        <div className="mx-auto max-w-4xl px-6 py-6">
          <CommandDeck />
          <div className="my-5 border-t border-border/40" />
          <TheWire />
        </div>
      </div>
    </div>
  );
}
