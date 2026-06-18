import { Check, Pencil, Trash2, X } from "lucide-react";
import { useMemo, useState } from "react";
import { BrainGraph } from "~/components/brain/BrainGraph";
import { Button } from "~/components/ui/button";
import { Dialog, DialogContent, DialogHeader, DialogTitle } from "~/components/ui/dialog";
import { Textarea } from "~/components/ui/textarea";
import { type Memory, NEEDS_CONFIRMATION_SCORE } from "~/lib/brain-api";

// MemoryReview is the dedicated review surface for the brain's least-trusted facts —
// the confirm/review queue. It walks the queue one fact at a time: the full text (not
// truncated), why it's queued, its provenance, its confidence, an isolated subgraph of
// the fact in its neighbourhood, and Confirm / Edit / Delete actions. This replaces
// trying to evaluate truncated one-liners in the graph sidebar.
export function MemoryReview({
  queue,
  allMemories,
  labelForScope,
  onConfirm,
  onDelete,
  onUpdate,
  onClose,
}: {
  // Snapshot of the review-queue memories, frozen by the parent at open time (it only
  // mounts this component while reviewing), so confirming/deleting doesn't reshuffle.
  queue: Memory[];
  allMemories: Memory[];
  labelForScope: (scope: string) => string;
  onConfirm: (id: string) => Promise<void> | void;
  onDelete: (id: string) => Promise<void> | void;
  onUpdate: (id: string, input: { text?: string }) => Promise<void> | void;
  onClose: () => void;
}) {
  const [cursor, setCursor] = useState(0);
  const [editing, setEditing] = useState(false);
  const [draft, setDraft] = useState("");
  const [busy, setBusy] = useState(false);

  const byId = useMemo(() => new Map(allMemories.map((m) => [m.id, m])), [allMemories]);
  const total = queue.length;
  const atEnd = cursor >= total;
  const current = atEnd ? null : (byId.get(queue[cursor]?.id ?? "") ?? queue[cursor] ?? null);

  // The isolated neighbourhood: the fact under review plus its 1-hop graph neighbours
  // (related either direction, and any resolvable provenance), so it's reviewed in
  // context rather than in a vacuum. Falls back to just the node when it stands alone.
  const subgraph = useMemo<Memory[]>(() => {
    if (!current) return [];
    const keep = new Set<string>([current.id]);
    const out = new Set(current.related ?? []);
    for (const d of current.derivedFrom ?? []) out.add(d);
    for (const m of allMemories) {
      if (m.related?.includes(current.id) || m.derivedFrom?.includes(current.id)) keep.add(m.id);
    }
    for (const id of out) keep.add(id);
    return allMemories.filter((m) => keep.has(m.id));
  }, [current, allMemories]);

  // The fact's resolvable neighbours, shown readably in the details pane (the subsumed
  // copies a promotion generalized are deleted, so these are its surviving links).
  const relatedFacts = useMemo(
    () => (current ? subgraph.filter((m) => m.id !== current.id) : []),
    [subgraph, current],
  );

  const advance = () => {
    setEditing(false);
    setCursor((c) => c + 1);
  };

  const act = async (fn: () => Promise<void> | void) => {
    if (busy) return;
    setBusy(true);
    try {
      await fn();
      advance();
    } finally {
      setBusy(false);
    }
  };

  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent className="flex h-[85vh] max-h-[85vh] w-[min(96vw,1240px)] max-w-none flex-col gap-0 p-0 sm:max-w-none">
        <DialogHeader className="border-b px-5 py-3">
          <DialogTitle className="flex items-center gap-2 text-base">
            Review memories
            <span className="text-sm font-normal text-muted-foreground">
              {atEnd ? `${total} reviewed` : `${cursor + 1} of ${total}`}
            </span>
          </DialogTitle>
        </DialogHeader>

        {atEnd || !current ? (
          <div className="flex flex-1 flex-col items-center justify-center gap-3 text-sm text-muted-foreground">
            <Check className="size-8 text-emerald-500" />
            Nothing left to review — the queue is clear.
            <Button variant="outline" size="sm" onClick={onClose}>
              Done
            </Button>
          </div>
        ) : (
          <div className="grid flex-1 grid-cols-1 overflow-hidden md:grid-cols-[5fr_6fr]">
            {/* Left: the isolated subgraph — the fact in its neighbourhood. Hidden on
                narrow widths so the fact + actions get the full column. */}
            <div className="relative hidden min-h-[16rem] border-r bg-background md:block">
              {subgraph.length >= 2 ? (
                <BrainGraph
                  compact
                  focusId={current.id}
                  memories={subgraph}
                  report={null}
                  labelForScope={labelForScope}
                  onConfirm={() => {}}
                />
              ) : (
                <div className="flex h-full items-center justify-center px-6 text-center text-sm text-muted-foreground">
                  This fact stands alone — no related memories yet (a knowledge gap).
                </div>
              )}
            </div>

            {/* Right: the fact + full context, with a pinned action bar. */}
            <div className="flex min-h-0 flex-col">
              <div className="flex-1 space-y-4 overflow-y-auto p-6">
                <div className="flex flex-wrap items-center gap-2 text-xs text-muted-foreground">
                  <span className="rounded bg-muted px-1.5 py-0.5">
                    {labelForScope(current.scope)}
                  </span>
                  <span className="rounded bg-muted px-1.5 py-0.5">{current.category}</span>
                  <ConfidenceBadge memory={current} />
                </div>

                {editing ? (
                  <Textarea
                    value={draft}
                    onChange={(e) => setDraft(e.target.value)}
                    rows={6}
                    className="text-base"
                  />
                ) : (
                  <p className="max-w-prose whitespace-pre-wrap text-base font-medium leading-relaxed text-foreground">
                    {current.text}
                  </p>
                )}

                <StatusBanner memory={current} />
                <WhyQueued memory={current} />
                <RelatedFacts items={relatedFacts} labelForScope={labelForScope} />
                <MetaRow memory={current} />
                {!editing && <Outcomes />}
              </div>

              <div className="flex flex-wrap items-center gap-2 border-t p-4">
                {editing ? (
                  <>
                    <Button
                      disabled={busy || !draft.trim()}
                      onClick={() => act(() => onUpdate(current.id, { text: draft.trim() }))}
                    >
                      Save as ground truth
                    </Button>
                    <Button variant="ghost" onClick={() => setEditing(false)}>
                      Cancel
                    </Button>
                  </>
                ) : (
                  <>
                    <Button disabled={busy} onClick={() => act(() => onConfirm(current.id))}>
                      <Check className="mr-1 size-4" /> Confirm
                    </Button>
                    <Button
                      variant="outline"
                      onClick={() => {
                        setDraft(current.text);
                        setEditing(true);
                      }}
                    >
                      <Pencil className="mr-1 size-4" /> Edit
                    </Button>
                    <Button
                      variant="destructive"
                      disabled={busy}
                      onClick={() => act(() => onDelete(current.id))}
                    >
                      <Trash2 className="mr-1 size-4" /> Delete
                    </Button>
                    <Button variant="ghost" className="ml-auto" onClick={advance}>
                      Skip <X className="ml-1 size-4" />
                    </Button>
                  </>
                )}
              </div>
            </div>
          </div>
        )}
      </DialogContent>
    </Dialog>
  );
}

function ConfidenceBadge({ memory }: { memory: Memory }) {
  const pct = Math.round((memory.confidenceScore ?? 0) * 100);
  const tone = memory.confidence === "ambiguous" ? "text-amber-600" : "text-muted-foreground";
  return (
    <span className={tone} title={`Confidence tier: ${memory.confidence ?? "inferred"}`}>
      {memory.confidence ?? "inferred"} · {pct}%
    </span>
  );
}

// StatusBanner makes the fact's CURRENT state — and what's at stake — obvious: an
// unverified inferred fact is at risk of being auto-rewritten or decayed until a human
// confirms it; a flagged one needs a decision now.
function StatusBanner({ memory }: { memory: Memory }) {
  const pct = Math.round((memory.confidenceScore ?? 0) * 100);
  if (memory.reviewNote) {
    return (
      <div className="rounded-md border border-red-500/40 bg-red-500/10 p-3 text-sm text-red-600 dark:text-red-400">
        <div className="font-medium">Flagged as wrong by an agent</div>
        <div className="mt-0.5 text-xs">It needs your decision — keep, fix, or remove it.</div>
      </div>
    );
  }
  return (
    <div className="rounded-md border border-amber-500/40 bg-amber-500/10 p-3 text-sm text-amber-700 dark:text-amber-300">
      <div className="font-medium">Unverified — the brain's own guess ({pct}%)</div>
      <div className="mt-0.5 text-xs">
        Not yet confirmed by you, so consolidation may rewrite or eventually forget it. Confirming
        locks it in as fact.
      </div>
    </div>
  );
}

// WhyQueued explains why this fact is in the review queue + its provenance.
function WhyQueued({ memory }: { memory: Memory }) {
  const reasons: string[] = [];
  if (memory.confidence === "ambiguous") {
    reasons.push("Marked ambiguous — confidence fell below the trusted band.");
  } else if (
    memory.scope === "global" &&
    (memory.confidenceScore ?? 1) <= NEEDS_CONFIRMATION_SCORE
  ) {
    reasons.push(
      "A cross-project generalization promoted to global — the riskiest kind of inference.",
    );
  } else if ((memory.confidenceScore ?? 1) <= NEEDS_CONFIRMATION_SCORE) {
    reasons.push("Low-confidence inferred fact.");
  }
  const subsumes = memory.derivedFrom?.length ?? 0;
  if (subsumes > 0) {
    reasons.push(`Generalizes ${subsumes} per-project memor${subsumes === 1 ? "y" : "ies"}.`);
  }
  if (reasons.length === 0) return null;

  return (
    <div className="rounded-md border bg-muted/40 p-3 text-xs text-muted-foreground">
      <div className="mb-1 font-medium text-foreground/80">Why you're seeing this</div>
      {reasons.map((r) => (
        <div key={r}>{r}</div>
      ))}
    </div>
  );
}

// RelatedFacts lists the fact's surviving graph neighbours with their full text, so the
// reviewer can judge it in context (the cramped sidebar only ever showed the node).
function RelatedFacts({
  items,
  labelForScope,
}: {
  items: Memory[];
  labelForScope: (scope: string) => string;
}) {
  if (items.length === 0) return null;
  return (
    <div className="text-xs">
      <div className="mb-1.5 font-medium text-foreground/80">Related facts ({items.length})</div>
      <ul className="space-y-1.5">
        {items.map((m) => (
          <li key={m.id} className="rounded-md border bg-card/50 p-2">
            <span className="mr-1 rounded bg-muted px-1 py-0.5 text-[10px] text-muted-foreground">
              {labelForScope(m.scope)}
            </span>
            <span className="text-foreground/90">{m.text}</span>
          </li>
        ))}
      </ul>
    </div>
  );
}

// MetaRow shows the provenance/usage facts a reviewer might want before deciding.
function MetaRow({ memory }: { memory: Memory }) {
  const updated = memory.updatedAt ? new Date(memory.updatedAt).toLocaleDateString() : null;
  return (
    <div className="flex flex-wrap gap-x-3 gap-y-0.5 text-[11px] text-muted-foreground">
      <span>source: {memory.source}</span>
      <span>used {memory.uses}×</span>
      {updated && <span>updated {updated}</span>}
    </div>
  );
}

// Outcomes spells out, per action, exactly what will happen — so Confirm/Edit/Delete
// aren't a guess. Sits right above the action bar.
function Outcomes() {
  return (
    <div className="space-y-1.5 rounded-md border bg-muted/30 p-3 text-xs">
      <div className="font-medium text-foreground/80">What each action does</div>
      <div className="flex gap-2">
        <Check className="mt-0.5 size-3.5 shrink-0 text-emerald-500" />
        <span>
          <b className="text-foreground/90">Confirm</b> — marks it ground truth (100%); never
          auto-rewritten or decayed again. Use for facts that are correct and truly cross-project.
        </span>
      </div>
      <div className="flex gap-2">
        <Pencil className="mt-0.5 size-3.5 shrink-0 text-sky-500" />
        <span>
          <b className="text-foreground/90">Edit</b> — rewrite it, then it's saved as your ground
          truth. Use when it's close but imprecise.
        </span>
      </div>
      <div className="flex gap-2">
        <Trash2 className="mt-0.5 size-3.5 shrink-0 text-red-500" />
        <span>
          <b className="text-foreground/90">Delete</b> — removes it permanently. Use for wrong or
          over-generalized facts.
        </span>
      </div>
    </div>
  );
}
