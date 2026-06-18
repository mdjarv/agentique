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

            {/* Right: the fact itself + why it's here + actions. */}
            <div className="flex flex-col overflow-y-auto p-6">
              <div className="mb-3 flex flex-wrap items-center gap-2 text-xs text-muted-foreground">
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
                  className="mb-4 text-base"
                />
              ) : (
                <p className="mb-4 max-w-prose whitespace-pre-wrap text-base font-medium leading-relaxed text-foreground">
                  {current.text}
                </p>
              )}

              <WhyQueued memory={current} />

              <div className="mt-auto flex flex-wrap gap-2 pt-4">
                {editing ? (
                  <>
                    <Button
                      size="sm"
                      disabled={busy || !draft.trim()}
                      onClick={() => act(() => onUpdate(current.id, { text: draft.trim() }))}
                    >
                      Save & next
                    </Button>
                    <Button size="sm" variant="ghost" onClick={() => setEditing(false)}>
                      Cancel
                    </Button>
                  </>
                ) : (
                  <>
                    <Button
                      size="sm"
                      disabled={busy}
                      onClick={() => act(() => onConfirm(current.id))}
                    >
                      <Check className="mr-1 size-4" /> Confirm
                    </Button>
                    <Button
                      size="sm"
                      variant="outline"
                      onClick={() => {
                        setDraft(current.text);
                        setEditing(true);
                      }}
                    >
                      <Pencil className="mr-1 size-4" /> Edit
                    </Button>
                    <Button
                      size="sm"
                      variant="outline"
                      disabled={busy}
                      onClick={() => act(() => onDelete(current.id))}
                    >
                      <Trash2 className="mr-1 size-4" /> Delete
                    </Button>
                    <Button size="sm" variant="ghost" className="ml-auto" onClick={advance}>
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

// WhyQueued explains, in one line, why this fact is in the review queue — the thing the
// truncated sidebar never told you.
function WhyQueued({ memory }: { memory: Memory }) {
  const reasons: string[] = [];
  if (memory.reviewNote) reasons.push(`Flagged as contradicted: "${memory.reviewNote}"`);
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

  return (
    <div className="rounded-md border bg-muted/40 p-2 text-xs text-muted-foreground">
      <div className="mb-1 font-medium text-foreground/80">Why you're seeing this</div>
      {reasons.map((r) => (
        <div key={r}>{r}</div>
      ))}
      {subsumes > 0 && (
        <div>
          Generalizes {subsumes} per-project memor{subsumes === 1 ? "y" : "ies"}.
        </div>
      )}
      <div className="mt-1">
        Confirm keeps it as ground truth (exempt from decay/rewrite); Delete drops it; Edit refines
        it.
      </div>
    </div>
  );
}
