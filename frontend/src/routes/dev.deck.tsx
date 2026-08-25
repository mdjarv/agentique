/**
 * Every "Needs you" card on one page, in the deck's real column width.
 *
 * The band mixes three reasons a session lands on the overview, and two of them
 * (an approval, an open question) only appear while an agent is genuinely
 * blocked — so the live deck can't be used to eyeball them. Presentational
 * only: rows are hand-built here.
 */
import { createFileRoute } from "@tanstack/react-router";
import { AttentionCard } from "~/components/home/AttentionCard";
import type { DeckRow } from "~/components/home/use-deck-rows";

export const Route = createFileRoute("/dev/deck")({
  component: DevDeck,
});

function row(overrides: Partial<DeckRow>): DeckRow {
  return {
    sessionId: crypto.randomUUID(),
    name: "Upgrade claudecli-go to v0.3.0",
    projectSlug: "agentkit",
    projectLabel: "agentkit",
    projectColorFg: "#73daca",
    kind: "unread",
    summary: "",
    restToken: "",
    lastActivity: Date.now(),
    ...overrides,
  };
}

const CARDS: { label: string; row: DeckRow }[] = [
  {
    label: "approval — the amber card, inline resolve",
    row: row({
      name: "Seat-map race condition",
      projectLabel: "alltix-api",
      projectColorFg: "#ff9e64",
      kind: "approval",
      summary: "Bash · go test -race ./ws/...",
      approvalId: "appr-1",
    }),
  },
  {
    label: "question — same amber, its own glyph",
    row: row({
      name: "Multi Machine Versioning",
      projectLabel: "agentique",
      projectColorFg: "#7aa2f7",
      kind: "question",
      summary: '"Which auth method should pairing use?"',
    }),
  },
  {
    label: "unread — a finished turn nobody has read",
    row: row({
      name: "Session UI improvements",
      projectLabel: "agentique",
      projectColorFg: "#7aa2f7",
      restToken: "finished",
    }),
  },
  {
    // The deck's half of the sidebar's rule: the process leaving is worth a
    // mark, never a greying — one message wakes the session again.
    label: "unread — the run was interrupted",
    row: row({
      name: "Stop button + live context meter",
      projectLabel: "agentique",
      projectColorFg: "#7aa2f7",
      restToken: "stopped",
    }),
  },
  {
    label: "unread — the CLI was reclaimed",
    row: row({ restToken: "evicted" }),
  },
  {
    label: "unread — that machine is unreachable",
    row: row({ restToken: "away" }),
  },
];

function noop() {}

function DevDeck() {
  return (
    <div className="h-full overflow-y-auto bg-background p-8">
      <h1 className="mb-6 text-lg font-semibold text-foreground-bright">Deck attention cards</h1>
      <div className="flex flex-col gap-4">
        {CARDS.map(({ label, row: cardRow }) => (
          <div key={label} className="flex flex-col gap-1">
            <span className="font-mono text-[10px] uppercase tracking-wider text-muted-foreground-faint">
              {label}
            </span>
            <div className="flex">
              <AttentionCard row={cardRow} onOpen={noop} />
            </div>
          </div>
        ))}
      </div>
    </div>
  );
}
