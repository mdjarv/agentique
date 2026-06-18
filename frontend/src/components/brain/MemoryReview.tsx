import { ArrowDown, Check, Loader2, Pencil, Sparkles, Trash2, X } from "lucide-react";
import { useMemo, useState } from "react";
import { Button } from "~/components/ui/button";
import { Dialog, DialogContent, DialogHeader, DialogTitle } from "~/components/ui/dialog";
import { Input } from "~/components/ui/input";
import { Textarea } from "~/components/ui/textarea";
import { type Memory, NEEDS_CONFIRMATION_SCORE } from "~/lib/brain-api";

// Preset one-click refine instructions (the chips). Free-text covers the rest.
const REFINE_PRESETS: { label: string; instruction: string }[] = [
  { label: "Tighten", instruction: "Make it more concise without losing meaning." },
  { label: "Generalize less", instruction: "Make it less broad — more specific and accurate." },
  { label: "Rephrase", instruction: "Rephrase it more clearly." },
];

// MemoryReview is the dedicated review surface for the brain's least-trusted facts.
// For a cross-scope promotion it frames the decision as a merge proposal — the input
// per-project facts, the synthesized output, and an explicit "do you agree?" — so the
// reviewer sees that the output is a generated join, not a copy. For a plain
// low-confidence fact it just shows the fact. Confirm / Edit / Delete per item.
export function MemoryReview({
  queue,
  allMemories,
  labelForScope,
  onConfirm,
  onDelete,
  onUpdate,
  onRefine,
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
  // onRefine asks the model to rewrite `text` per `instruction` and resolves to the
  // draft (no save). Errors are surfaced by the caller; reject to leave the draft.
  onRefine: (id: string, text: string, instruction: string) => Promise<string>;
  onClose: () => void;
}) {
  const [cursor, setCursor] = useState(0);
  const [editing, setEditing] = useState(false);
  const [draft, setDraft] = useState("");
  const [busy, setBusy] = useState(false);
  const [refining, setRefining] = useState(false);
  const [instruction, setInstruction] = useState("");

  const byId = useMemo(() => new Map(allMemories.map((m) => [m.id, m])), [allMemories]);
  const total = queue.length;
  const atEnd = cursor >= total;
  const current = atEnd ? null : (byId.get(queue[cursor]?.id ?? "") ?? queue[cursor] ?? null);

  const inputs = current?.subsumed ?? [];
  const derivedCount = current?.derivedFrom?.length ?? 0;
  // A promotion is anything that merged project facts in (whether or not the source
  // snapshots survived). Drives the inputs → output → "agree?" framing.
  const isPromotion = inputs.length > 0 || derivedCount > 0;

  const advance = () => {
    setEditing(false);
    setInstruction("");
    setCursor((c) => c + 1);
  };

  // runRefine asks the model to rewrite the current draft per an instruction and drops
  // the result back into the editable draft (the user then Saves or refines again).
  const runRefine = async (instr: string) => {
    if (!current || refining || !instr.trim()) return;
    setRefining(true);
    try {
      const next = await onRefine(current.id, draft || current.text, instr);
      if (next?.trim()) setDraft(next.trim());
    } catch {
      // The caller surfaces the error (toast); leave the draft untouched.
    } finally {
      setRefining(false);
    }
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
      <DialogContent className="flex h-[85vh] max-h-[85vh] w-[min(92vw,820px)] max-w-none flex-col gap-0 p-0 sm:max-w-none">
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
          <div className="flex min-h-0 flex-1 flex-col">
            <div className="flex-1 space-y-4 overflow-y-auto p-6">
              {/* INPUTS — the per-project facts this promotion merged together. */}
              {isPromotion && (
                <div className="space-y-2">
                  <div className="text-[11px] font-medium uppercase tracking-wide text-muted-foreground">
                    {inputs.length > 0
                      ? `Input memories (${inputs.length}) — merged from these`
                      : "Merged from per-project facts"}
                  </div>
                  {inputs.length > 0 ? (
                    <ul className="space-y-1.5">
                      {inputs.map((s) => (
                        <li
                          key={`${s.scope}::${s.text}`}
                          className="rounded-md border bg-card/50 p-2 text-sm"
                        >
                          <span className="mr-1.5 rounded bg-muted px-1 py-0.5 text-[10px] text-muted-foreground">
                            {labelForScope(s.scope)}
                          </span>
                          <span className="text-foreground/90">{s.text}</span>
                        </li>
                      ))}
                    </ul>
                  ) : (
                    <p className="rounded-md border border-dashed p-2 text-xs text-muted-foreground">
                      Merged from {derivedCount} project fact{derivedCount === 1 ? "" : "s"}, but
                      the originals weren't retained (they predate source capture). Review the
                      generated statement below.
                    </p>
                  )}
                  <div className="flex items-center justify-center gap-1.5 py-0.5 text-[11px] text-muted-foreground">
                    <ArrowDown className="size-3.5" />
                    merged &amp; promoted into
                  </div>
                </div>
              )}

              {/* OUTPUT — the fact that will actually be saved. */}
              <div>
                <div className="mb-1 text-[11px] font-medium uppercase tracking-wide text-muted-foreground">
                  {isPromotion ? "Proposed global memory" : "The fact, as it will be saved"}
                </div>
                <div className="mb-2 flex flex-wrap items-center gap-2 text-xs text-muted-foreground">
                  <span className="rounded bg-muted px-1.5 py-0.5">
                    {labelForScope(current.scope)}
                  </span>
                  <span className="rounded bg-muted px-1.5 py-0.5">{current.category}</span>
                  <ConfidenceBadge memory={current} />
                </div>
                {editing ? (
                  <div className="space-y-2">
                    <Textarea
                      value={draft}
                      onChange={(e) => setDraft(e.target.value)}
                      rows={5}
                      className="text-base"
                    />
                    <div className="space-y-2 rounded-md border bg-muted/30 p-2">
                      <div className="flex items-center gap-1.5 text-[11px] font-medium text-foreground/80">
                        <Sparkles className="size-3.5" /> Refine with AI
                        {refining && <Loader2 className="size-3.5 animate-spin" />}
                      </div>
                      <div className="flex flex-wrap gap-1.5">
                        {REFINE_PRESETS.map((p) => (
                          <button
                            key={p.label}
                            type="button"
                            disabled={refining}
                            onClick={() => runRefine(p.instruction)}
                            className="rounded-full border px-2 py-0.5 text-xs hover:bg-muted disabled:opacity-50"
                          >
                            {p.label}
                          </button>
                        ))}
                      </div>
                      <div className="flex gap-1.5">
                        <Input
                          value={instruction}
                          onChange={(e) => setInstruction(e.target.value)}
                          onKeyDown={(e) => {
                            if (e.key === "Enter") {
                              e.preventDefault();
                              runRefine(instruction);
                            }
                          }}
                          placeholder="or describe the change… (e.g. 'it's Go-only')"
                          className="h-8 flex-1 text-sm"
                        />
                        <Button
                          size="sm"
                          variant="outline"
                          disabled={refining || !instruction.trim()}
                          onClick={() => runRefine(instruction)}
                        >
                          Refine
                        </Button>
                      </div>
                    </div>
                  </div>
                ) : (
                  <p className="whitespace-pre-wrap rounded-md border border-primary/30 bg-primary/5 p-3 text-base font-medium leading-relaxed text-foreground">
                    {current.text}
                  </p>
                )}
              </div>

              <StatusBanner memory={current} />
              <WhyQueued memory={current} />
              <MetaRow memory={current} />
              {!editing && <Outcomes />}
            </div>

            <div className="space-y-2 border-t p-4">
              {!editing && isPromotion && (
                <div className="text-sm font-medium">
                  Do you agree with this merge &amp; promotion to global?
                </div>
              )}
              <div className="flex flex-wrap items-center gap-2">
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

// StatusBanner makes the fact's CURRENT state — and what's at stake — obvious.
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

// WhyQueued explains why this fact is in the review queue.
function WhyQueued({ memory }: { memory: Memory }) {
  const reasons: string[] = [];
  if (memory.confidence === "ambiguous") {
    reasons.push("Marked ambiguous — confidence fell below the trusted band.");
  } else if (
    memory.scope === "global" &&
    (memory.confidenceScore ?? 1) <= NEEDS_CONFIRMATION_SCORE
  ) {
    reasons.push(
      "A cross-project generalization promoted to global — a generated summary, not a copy, so it needs your check.",
    );
  } else if ((memory.confidenceScore ?? 1) <= NEEDS_CONFIRMATION_SCORE) {
    reasons.push("Low-confidence inferred fact.");
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
// aren't a guess.
function Outcomes() {
  return (
    <div className="space-y-1.5 rounded-md border bg-muted/30 p-3 text-xs">
      <div className="font-medium text-foreground/80">What each action does</div>
      <div className="flex gap-2">
        <Check className="mt-0.5 size-3.5 shrink-0 text-emerald-500" />
        <span>
          <b className="text-foreground/90">Confirm</b> — keeps the statement above{" "}
          <b className="text-foreground/90">exactly as written</b> and marks it ground truth (100%);
          never auto-rewritten or decayed again.
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
