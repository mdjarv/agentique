import { ArrowDown, Check, Loader2, Pencil, Sparkles, Trash2, X } from "lucide-react";
import { useState } from "react";
import { MemoryLabels } from "~/components/brain/MemoryLabels";
import { Button } from "~/components/ui/button";
import { Dialog, DialogContent, DialogHeader, DialogTitle } from "~/components/ui/dialog";
import { Input } from "~/components/ui/input";
import { Textarea } from "~/components/ui/textarea";
import type { Memory } from "~/lib/brain-api";
import { scopeColor } from "~/lib/scope-color";

// Preset one-click refine instructions (the chips). Free-text covers the rest.
const REFINE_PRESETS: { label: string; instruction: string }[] = [
  { label: "Tighten", instruction: "Make it more concise without losing meaning." },
  { label: "Generalize less", instruction: "Make it less broad — more specific and accurate." },
  { label: "Rephrase", instruction: "Rephrase it more clearly." },
];

// MemoryReview is the dedicated review surface for the brain's least-trusted facts.
// Hierarchy (Refactoring UI): the proposed FACT is the one focal point; the merge
// inputs are secondary context; state/why/metadata are tertiary muted text — separated
// by spacing and a single left-accent, not stacked boxes. For a cross-scope promotion
// it reads as a merge proposal (inputs → output → "agree?"); otherwise just the fact.
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

  const total = queue.length;
  const atEnd = cursor >= total;
  const queued = atEnd ? null : (queue[cursor] ?? null);
  // Look up the live record so edits/confirms reflect immediately; fall back to the
  // frozen snapshot if it was just removed.
  const current = queued ? (allMemories.find((m) => m.id === queued.id) ?? queued) : null;

  const advance = () => {
    setEditing(false);
    setInstruction("");
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

  const pct = current ? Math.round((current.confidenceScore ?? 0) * 100) : 0;
  const flagged = !!current?.reviewNote;
  const inputs = current?.subsumed ?? [];
  const derivedCount = current?.derivedFrom?.length ?? 0;
  const isPromotion = inputs.length > 0 || derivedCount > 0;

  // The left-accent colour carries the trust state, so no separate status box.
  const accent = flagged ? "border-red-500/70" : "border-amber-500/70";
  const stateLabel = flagged ? "flagged as wrong" : `unverified · ${pct}%`;
  const whyLine = flagged
    ? `Flagged: "${current?.reviewNote}"`
    : isPromotion || current?.scope === "global"
      ? "A generated cross-project summary, not a copy — confirm to lock it in as fact."
      : "A low-confidence guess — confirm to lock it in as fact.";
  const updated = current?.updatedAt ? new Date(current.updatedAt).toLocaleDateString() : null;

  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent className="flex h-[85vh] max-h-[85vh] w-[min(92vw,720px)] max-w-none flex-col gap-0 p-0 sm:max-w-none">
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
            <div className="flex-1 space-y-5 overflow-y-auto px-6 py-5">
              {/* INPUTS — secondary context. Plain list, no per-item chrome. */}
              {isPromotion && (
                <div className="space-y-1.5">
                  <div className="text-xs font-medium text-muted-foreground">
                    Merged from {inputs.length > 0 ? inputs.length : derivedCount} project fact
                    {(inputs.length > 0 ? inputs.length : derivedCount) === 1 ? "" : "s"}
                  </div>
                  {inputs.length > 0 ? (
                    <ul className="space-y-3">
                      {inputs.map((s) => (
                        <li
                          key={`${s.scope}::${s.text}`}
                          className="border-l-2 pl-3"
                          style={{ borderColor: scopeColor(s.scope) }}
                        >
                          <div
                            className="text-[10px] font-semibold uppercase tracking-wide"
                            style={{ color: scopeColor(s.scope) }}
                          >
                            {labelForScope(s.scope)}
                          </div>
                          <div className="text-sm text-muted-foreground">{s.text}</div>
                        </li>
                      ))}
                    </ul>
                  ) : (
                    <div className="text-xs italic text-muted-foreground/70">
                      Originals not retained (they predate source capture).
                    </div>
                  )}
                  <div className="flex items-center gap-1 pt-0.5 text-[11px] text-muted-foreground/60">
                    <ArrowDown className="size-3" /> merged &amp; promoted into
                  </div>
                </div>
              )}

              {/* THE FACT — the one focal point. Left-accent colour = trust state. */}
              <div className={`border-l-4 pl-4 ${accent}`}>
                <div className="text-[11px] font-medium uppercase tracking-wide text-muted-foreground">
                  {isPromotion ? "Proposed global memory" : "Memory"}
                </div>

                {editing ? (
                  <div className="mt-2 space-y-2">
                    <Textarea
                      value={draft}
                      onChange={(e) => setDraft(e.target.value)}
                      rows={4}
                      className="text-base"
                    />
                    <div className="space-y-2">
                      <div className="flex items-center gap-1.5 text-[11px] font-medium text-muted-foreground">
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
                            className="rounded-full border px-2 py-0.5 text-xs text-muted-foreground hover:bg-muted hover:text-foreground disabled:opacity-50"
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
                  <>
                    <p className="mt-1 text-lg font-medium leading-snug text-foreground">
                      {current.text}
                    </p>
                    <div className="mt-1.5 text-xs text-muted-foreground">
                      {labelForScope(current.scope)} · {current.category} · {stateLabel}
                    </div>
                    <p className="mt-1 text-xs text-muted-foreground">{whyLine}</p>
                  </>
                )}
              </div>

              {/* METADATA — tertiary, fine print. Evidence/volatility chips render here
                  read-only (F1); they show only when a fact deviates from the defaults. */}
              <div className="flex flex-wrap items-center gap-x-1.5 gap-y-1 text-[11px] text-muted-foreground/60">
                <span>
                  {current.source} · used {current.uses}×{updated ? ` · updated ${updated}` : ""}
                </span>
                <MemoryLabels memory={current} />
              </div>
            </div>

            <div className="space-y-2 border-t px-5 py-4">
              {!editing && (
                <div className="text-sm">
                  {isPromotion
                    ? "Do you agree with this merge & promotion to global?"
                    : "Keep this fact?"}{" "}
                  <span className="text-xs text-muted-foreground">
                    Confirm keeps it as-is (trusted) · Edit rewords it · Delete removes it.
                  </span>
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
                      variant="ghost"
                      disabled={busy}
                      className="text-red-600 hover:bg-red-500/10 hover:text-red-600 dark:text-red-400"
                      onClick={() => act(() => onDelete(current.id))}
                    >
                      <Trash2 className="mr-1 size-4" /> Delete
                    </Button>
                    <Button
                      variant="ghost"
                      className="ml-auto text-muted-foreground"
                      onClick={advance}
                    >
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
