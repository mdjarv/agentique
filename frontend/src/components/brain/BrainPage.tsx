import {
  ArrowUpToLine,
  Brain,
  Check,
  ChevronRight,
  ClipboardCheck,
  List,
  Loader2,
  Lock,
  LockOpen,
  Network,
  Orbit,
  Pin,
  PinOff,
  Plus,
  Sparkles,
  Trash2,
} from "lucide-react";
import { lazy, Suspense, useEffect, useMemo, useState } from "react";
import { toast } from "sonner";
import { BrainGraph } from "~/components/brain/BrainGraph";
import { MemoryReview } from "~/components/brain/MemoryReview";
import { PageHeader } from "~/components/layout/PageHeader";
import { Badge } from "~/components/ui/badge";
import { Button } from "~/components/ui/button";
import { Input } from "~/components/ui/input";
import { Textarea } from "~/components/ui/textarea";
import {
  type ConsolidateMode,
  type ConsolidateReport,
  inReviewQueue,
  type Memory,
  needsConfirmation,
  refineMemory,
} from "~/lib/brain-api";
import { getErrorMessage } from "~/lib/utils";
import { useAppStore } from "~/stores/app-store";
import { useBrainStore } from "~/stores/brain-store";

// Code-split the 3D view: three.js + d3-force-3d are a large bundle only the 3D graph
// needs, so they load on demand when the user opens it — the list/2D paths stay lean.
const BrainGraph3D = lazy(() =>
  import("~/components/brain/BrainGraph3D").then((m) => ({ default: m.BrainGraph3D })),
);

const CATEGORIES = ["fact", "identity", "preference", "contact", "project", "goal", "task"];
const GLOBAL_SCOPE = "global";
const MODELS = ["opus", "sonnet", "haiku"];
const CONSOLIDATE_MODES: { value: ConsolidateMode; label: string }[] = [
  { value: "conservative", label: "Conservative" },
  { value: "aggressive", label: "Aggressive" },
];

export function BrainPage() {
  const {
    memories,
    semantic,
    loaded,
    load,
    graph,
    graphLoading,
    loadGraph,
    confirm,
    update,
    remove,
    flareSeq,
    create,
    preview,
    previewScope,
    previewing,
    applying,
    progress,
    startPreview,
    applyPreview,
    dismissPreview,
    globalPreview,
    globalPreviewing,
    globalApplying,
    startGlobalConsolidate,
    applyGlobalPreview,
    dismissGlobalPreview,
    consolidatingAll,
    startConsolidateAll,
    hydrateJob,
  } = useBrainStore();
  const projects = useAppStore((s) => s.projects);
  const [filter, setFilter] = useState("");
  const [adding, setAdding] = useState(false);
  const [model, setModel] = useState("opus");
  const [mode, setMode] = useState<ConsolidateMode>("conservative");
  // Default to the graph: it's the more legible, exploratory entry point to the brain
  // (the list is one click away via the toggle). "graph3d" is the three.js view —
  // memories orbiting a central brain model in true 3D.
  const [view, setView] = useState<"list" | "graph" | "graph3d">("graph");
  const graphView = view === "graph" || view === "graph3d";
  const [reviewing, setReviewing] = useState(false);
  // The review queue: the brain's least-trusted / flagged facts (RFC P2 confirm + D2).
  const reviewQueue = useMemo(() => memories.filter(inReviewQueue), [memories]);
  // Global is expanded by default; projects collapse to keep the (large) list
  // navigable. An active filter force-expands everything so matches are visible.
  const [expanded, setExpanded] = useState<Set<string>>(() => new Set([GLOBAL_SCOPE]));
  const filtering = filter.trim().length > 0;
  const toggleScope = (scope: string) =>
    setExpanded((prev) => {
      const next = new Set(prev);
      if (next.has(scope)) next.delete(scope);
      else next.add(scope);
      return next;
    });

  useEffect(() => {
    if (!loaded) load();
    // Resync to any consolidation already running (e.g. started in another tab).
    hydrateJob();
  }, [loaded, load, hydrateJob]);

  // Load (and keep fresh) the centrality graph only while the graph view is open.
  // While a consolidation job is running it broadcasts a burst of brain.updated events
  // (one per scope), each of which would otherwise refetch the graph and re-lay it out;
  // we hold off until the job ends (jobActive flips false) and refresh once then, so the
  // live graph stays stable through a consolidation instead of churning on every step.
  const jobActive = previewing || globalPreviewing || consolidatingAll;
  // biome-ignore lint/correctness/useExhaustiveDependencies: flareSeq is a trigger-only dep — it bumps on any memory change (here or another tab) to re-fetch the graph so the insights + confirm queue stay fresh after a consolidation/confirm; its value isn't read in the body.
  useEffect(() => {
    if (graphView && !jobActive) loadGraph();
  }, [graphView, loadGraph, flareSeq, jobActive]);

  const labelForScope = useMemo(() => {
    const byId = new Map(projects.map((p) => [p.id, p.name]));
    return (scope: string) => {
      if (scope === GLOBAL_SCOPE) return "Global";
      if (scope.startsWith("project:")) {
        const id = scope.slice("project:".length);
        return byId.get(id) ?? `Project ${id.slice(0, 8)}`;
      }
      return scope;
    };
  }, [projects]);

  const groups = useMemo(() => {
    const f = filter.trim().toLowerCase();
    const filtered = f ? memories.filter((m) => m.text.toLowerCase().includes(f)) : memories;
    const byScope = new Map<string, Memory[]>();
    for (const m of filtered) {
      const arr = byScope.get(m.scope) ?? [];
      arr.push(m);
      byScope.set(m.scope, arr);
    }
    // Global is always present at the top, even when empty — it's where
    // cross-cutting knowledge lives, and the entry point to seed/promote it.
    if (!byScope.has(GLOBAL_SCOPE)) byScope.set(GLOBAL_SCOPE, []);
    // Stable ordering: global first, then alphabetical by label; pinned first within a scope.
    return [...byScope.entries()]
      .map(([scope, items]) => ({
        scope,
        items: [...items].sort((a, b) => Number(b.pinned) - Number(a.pinned) || b.uses - a.uses),
      }))
      .sort((a, b) => {
        if (a.scope === GLOBAL_SCOPE) return -1;
        if (b.scope === GLOBAL_SCOPE) return 1;
        return labelForScope(a.scope).localeCompare(labelForScope(b.scope));
      });
  }, [memories, filter, labelForScope]);

  // Graph view shows the same filtered set as the list, flat (grouping is expressed
  // by node color, not sections). It uses the centrality-annotated nodes from the
  // graph endpoint once loaded, falling back to the plain list while it loads.
  const graphMemories = useMemo(() => {
    const f = filter.trim().toLowerCase();
    const base = graph?.nodes ?? memories;
    return f ? base.filter((m) => m.text.toLowerCase().includes(f)) : base;
  }, [graph, memories, filter]);

  const handleConsolidate = async (scope: string, force = false) => {
    try {
      await startPreview(scope, model, mode, force);
    } catch (err) {
      toast.error(getErrorMessage(err, "Preview failed"));
    }
  };

  const handleApply = async () => {
    try {
      const changes = await applyPreview();
      toast.success(`Applied ${changes} change${changes === 1 ? "" : "s"}`);
    } catch (err) {
      toast.error(getErrorMessage(err, "Apply failed"));
    }
  };

  const handleGlobalConsolidate = async () => {
    try {
      await startGlobalConsolidate(model);
    } catch (err) {
      toast.error(getErrorMessage(err, "Global preview failed"));
    }
  };

  const handleApplyGlobal = async () => {
    try {
      const changes = await applyGlobalPreview();
      toast.success(`Promoted to global: ${changes} change${changes === 1 ? "" : "s"}`);
    } catch (err) {
      toast.error(getErrorMessage(err, "Global apply failed"));
    }
  };

  const handleConsolidateAll = async () => {
    try {
      await startConsolidateAll(model);
    } catch (err) {
      toast.error(getErrorMessage(err, "Consolidate all failed"));
    }
  };

  return (
    <div className="flex flex-col h-full">
      <PageHeader>
        <Brain className="size-4 text-primary" />
        <span className="font-semibold">Brain</span>
        <Badge variant={semantic ? "default" : "secondary"} className="ml-1">
          {semantic ? "Semantic" : "Keyword"}
        </Badge>
        <span className="ml-auto text-xs text-muted-foreground tabular-nums">
          {memories.length} {memories.length === 1 ? "memory" : "memories"}
        </span>
        <div className="flex items-center overflow-hidden rounded-md border">
          <button
            type="button"
            onClick={() => setView("list")}
            title="List view"
            className={`flex size-8 items-center justify-center ${view === "list" ? "bg-muted text-foreground" : "text-muted-foreground hover:text-foreground"}`}
          >
            <List className="size-4" />
          </button>
          <button
            type="button"
            onClick={() => setView("graph")}
            title="Graph view"
            className={`flex size-8 items-center justify-center ${view === "graph" ? "bg-muted text-foreground" : "text-muted-foreground hover:text-foreground"}`}
          >
            <Network className="size-4" />
          </button>
          <button
            type="button"
            onClick={() => setView("graph3d")}
            title="3D graph view — memories orbiting the brain"
            className={`flex size-8 items-center justify-center ${view === "graph3d" ? "bg-muted text-foreground" : "text-muted-foreground hover:text-foreground"}`}
          >
            <Orbit className="size-4" />
          </button>
        </div>
        <select
          value={model}
          onChange={(e) => setModel(e.target.value)}
          className="h-8 rounded-md border bg-background px-2 text-xs capitalize"
          title="Model used to reorganize when you Consolidate a scope"
        >
          {MODELS.map((m) => (
            <option key={m} value={m} className="capitalize">
              {m}
            </option>
          ))}
        </select>
        <select
          value={mode}
          onChange={(e) => setMode(e.target.value as ConsolidateMode)}
          className="h-8 rounded-md border bg-background px-2 text-xs"
          title="Per-scope consolidation strategy. Conservative merges only true duplicates; Aggressive collapses families of granular facts into broad rules to shrink a bloated scope (preview before applying)."
        >
          {CONSOLIDATE_MODES.map((m) => (
            <option key={m.value} value={m.value}>
              {m.label}
            </option>
          ))}
        </select>
        <Button
          size="sm"
          variant="outline"
          disabled={consolidatingAll}
          onClick={handleConsolidateAll}
          title={`Consolidate every scope with ${model} and apply automatically`}
        >
          {consolidatingAll ? (
            <Loader2 className="size-4 animate-spin" />
          ) : (
            <Sparkles className="size-4" />
          )}
          {consolidatingAll
            ? progress
              ? `Consolidating… ${progress.current}/${progress.total}`
              : "Consolidating…"
            : "Consolidate all"}
        </Button>
        <Button
          size="sm"
          variant="outline"
          disabled={reviewQueue.length === 0}
          onClick={() => setReviewing(true)}
          title="Review the brain's least-trusted facts: confirm, edit, or drop them"
        >
          <ClipboardCheck className="size-4" /> Review
          {reviewQueue.length > 0 && (
            <span className="ml-1 rounded bg-amber-500/20 px-1 text-amber-600 tabular-nums">
              {reviewQueue.length}
            </span>
          )}
        </Button>
        <Button size="sm" variant="outline" onClick={() => setAdding((v) => !v)}>
          <Plus className="size-4" /> Add
        </Button>
      </PageHeader>

      {reviewing && (
        <MemoryReview
          queue={reviewQueue}
          allMemories={memories}
          labelForScope={labelForScope}
          onClose={() => setReviewing(false)}
          onConfirm={async (id) => {
            try {
              await confirm(id);
              toast.success("Confirmed — kept as ground truth");
            } catch (err) {
              toast.error(getErrorMessage(err, "Failed to confirm"));
            }
          }}
          onDelete={async (id) => {
            try {
              await remove(id);
              toast.success("Deleted");
            } catch (err) {
              toast.error(getErrorMessage(err, "Failed to delete"));
            }
          }}
          onUpdate={async (id, input) => {
            try {
              await update(id, input);
              toast.success("Updated — kept as ground truth");
            } catch (err) {
              toast.error(getErrorMessage(err, "Failed to update"));
            }
          }}
          onRefine={async (id, text, instr) => {
            try {
              return await refineMemory(id, { text, instruction: instr, model });
            } catch (err) {
              toast.error(getErrorMessage(err, "Failed to refine"));
              throw err;
            }
          }}
        />
      )}

      <div className="px-4 py-2 border-b">
        <Input
          placeholder="Filter memories…"
          value={filter}
          onChange={(e) => setFilter(e.target.value)}
        />
      </div>

      {adding && (
        <AddMemoryForm
          projects={projects}
          onCancel={() => setAdding(false)}
          onSubmit={async (input) => {
            try {
              await create(input);
              toast.success("Memory added");
              setAdding(false);
            } catch (err) {
              toast.error(getErrorMessage(err, "Failed to add memory"));
            }
          }}
        />
      )}

      {view === "graph" && (
        <div className="min-h-0 flex-1">
          {/* Hold off mounting the graph until the payload (semantic edges + tuning) has loaded, so
              the force layout runs ONCE with the real edges from the first tick. Rendering early
              with the edge-less list would settle a layout, then reheat from those wrong positions
              when the edges arrive — the "settles then jumps to a mess" the similarity links cause.
              A graph fetch error (no graph, not loading) still falls through to the degraded list. */}
          {!graph && graphLoading ? (
            <div className="flex h-full items-center justify-center gap-2 text-sm text-muted-foreground">
              <Loader2 className="size-4 animate-spin" /> Laying out graph…
            </div>
          ) : (
            <BrainGraph
              memories={graphMemories}
              links={graph?.links ?? null}
              report={graph?.report ?? null}
              tuning={graph?.tuning ?? null}
              labelForScope={labelForScope}
              onConfirm={async (id) => {
                try {
                  await confirm(id);
                  toast.success("Confirmed — kept as ground truth");
                } catch (err) {
                  toast.error(getErrorMessage(err, "Failed to confirm"));
                }
              }}
            />
          )}
        </div>
      )}
      {view === "graph3d" && (
        <div className="min-h-0 flex-1">
          {!graph && graphLoading ? (
            <div className="flex h-full items-center justify-center gap-2 text-sm text-muted-foreground">
              <Loader2 className="size-4 animate-spin" /> Laying out graph…
            </div>
          ) : (
            <Suspense
              fallback={
                <div className="flex h-full items-center justify-center gap-2 text-sm text-muted-foreground">
                  <Loader2 className="size-4 animate-spin" /> Loading 3D view…
                </div>
              }
            >
              <BrainGraph3D
                memories={graphMemories}
                links={graph?.links ?? null}
                labelForScope={labelForScope}
              />
            </Suspense>
          )}
        </div>
      )}
      <div className="flex-1 overflow-y-auto p-4 space-y-6" hidden={view !== "list"}>
        {groups.length === 0 && (
          <div className="text-center text-sm text-muted-foreground py-12">
            {loaded
              ? "No memories yet. Agents add them via the memory tools, or add one manually."
              : "Loading…"}
          </div>
        )}
        {groups.map((g) => {
          const isPreviewScope = previewScope === g.scope;
          const isGlobal = g.scope === GLOBAL_SCOPE;
          const open = filtering || expanded.has(g.scope);
          return (
            <section key={g.scope}>
              <div className="flex items-center gap-2 mb-2">
                <button
                  type="button"
                  onClick={() => toggleScope(g.scope)}
                  className="flex items-center gap-2 min-w-0 text-left hover:text-foreground"
                >
                  <ChevronRight
                    className={`size-3.5 shrink-0 text-muted-foreground transition-transform ${open ? "rotate-90" : ""}`}
                  />
                  <h2 className="text-sm font-semibold truncate">{labelForScope(g.scope)}</h2>
                  <span className="text-xs text-muted-foreground tabular-nums">
                    {g.items.length}
                  </span>
                </button>
                <div className="ml-auto flex items-center gap-1">
                  {isGlobal && (
                    <Button
                      size="sm"
                      variant="ghost"
                      className="text-xs"
                      disabled={globalPreviewing}
                      onClick={handleGlobalConsolidate}
                      title={`Scan all projects with ${model} and promote cross-cutting facts (recurring conventions, your preferences) to global`}
                    >
                      <ArrowUpToLine className="size-3.5" />
                      {globalPreviewing ? "Scanning…" : "Lift to global"}
                    </Button>
                  )}
                  <Button
                    size="sm"
                    variant="ghost"
                    className="text-xs"
                    disabled={previewing && isPreviewScope}
                    onClick={() => handleConsolidate(g.scope)}
                    title={`Preview a consolidation with ${model}: merge duplicates, distill captures, decay stale facts`}
                  >
                    <Sparkles className="size-3.5" />
                    {previewing && isPreviewScope ? "Previewing…" : "Consolidate"}
                  </Button>
                </div>
              </div>
              {isGlobal && (globalPreviewing || globalPreview) && (
                <ConsolidatePreview
                  previewing={globalPreviewing}
                  applying={globalApplying}
                  progress={progress}
                  report={globalPreview?.report ?? null}
                  onApply={handleApplyGlobal}
                  onDismiss={dismissGlobalPreview}
                  emptyLabel="No cross-cutting facts to promote — global is up to date."
                />
              )}
              {isPreviewScope && (
                <ConsolidatePreview
                  previewing={previewing}
                  applying={applying}
                  progress={progress}
                  report={preview?.report ?? null}
                  onApply={handleApply}
                  onDismiss={dismissPreview}
                  onForceRerun={() => handleConsolidate(g.scope, true)}
                />
              )}
              {open &&
                (g.items.length > 0 ? (
                  <div className="space-y-2">
                    {g.items.map((m) => (
                      <MemoryCard key={m.id} memory={m} />
                    ))}
                  </div>
                ) : (
                  isGlobal && (
                    <p className="text-xs text-muted-foreground pl-5 pb-1">
                      No global memories yet — cross-cutting facts (your identity, durable
                      preferences, conventions across projects) live here and are recalled in every
                      session.
                    </p>
                  )
                ))}
            </section>
          );
        })}
      </div>
    </div>
  );
}

function ConsolidatePreview({
  previewing,
  applying,
  progress,
  report,
  onApply,
  onDismiss,
  onForceRerun,
  emptyLabel = "Already consolidated — nothing to change.",
}: {
  previewing: boolean;
  applying: boolean;
  progress: { current: number; total: number } | null;
  report: ConsolidateReport | null;
  onApply: () => void;
  onDismiss: () => void;
  // When set, an empty/skipped preview offers a "Force re-run" that re-tidies the
  // unchanged scope (e.g. to apply a different strategy or a newer algorithm).
  onForceRerun?: () => void;
  emptyLabel?: string;
}) {
  if (previewing) {
    return (
      <div className="mb-3 rounded-md border bg-muted/30 p-3 flex items-center gap-2 text-xs text-muted-foreground">
        <Loader2 className="size-3.5 animate-spin" />
        {progress
          ? `Analyzing memories… ${progress.current}/${progress.total}`
          : "Analyzing memories…"}
      </div>
    );
  }
  if (!report) return null;

  const changes =
    (report.promoted?.length ?? 0) +
    (report.rewritten?.length ?? 0) +
    (report.abstracted?.length ?? 0) +
    (report.deleted?.length ?? 0) +
    (report.decayed?.length ?? 0);

  if (report.reorgRefused) {
    return (
      <PreviewShell onDismiss={onDismiss}>
        <span className="text-xs text-amber-600">
          Skipped — this consolidation would remove more than half of the scope (safety limit).
        </span>
      </PreviewShell>
    );
  }
  if (report.skipped || changes === 0) {
    return (
      <PreviewShell
        onDismiss={onDismiss}
        action={
          onForceRerun ? (
            <Button
              size="sm"
              variant="ghost"
              className="text-xs"
              onClick={onForceRerun}
              title="Re-run the consolidation on this unchanged scope (e.g. with a different strategy or a newer algorithm)"
            >
              Force re-run
            </Button>
          ) : undefined
        }
      >
        <span className="text-xs text-muted-foreground">
          {report.skipped ? "Already tidied — unchanged since the last pass." : emptyLabel}
        </span>
      </PreviewShell>
    );
  }

  return (
    <div className="mb-3 rounded-md border bg-muted/30 p-3 space-y-2">
      <div className="flex items-center gap-2">
        <span className="text-xs font-medium">
          Preview · {changes} change{changes === 1 ? "" : "s"}
        </span>
        <div className="ml-auto flex gap-2">
          <Button size="sm" variant="ghost" onClick={onDismiss} disabled={applying}>
            Dismiss
          </Button>
          <Button size="sm" onClick={onApply} disabled={applying}>
            {applying ? "Applying…" : "Apply"}
          </Button>
        </div>
      </div>
      <ul className="space-y-1.5 text-xs">
        {report.rewritten?.map((c) => (
          <li key={`rw-${c.after.id}`} className="flex flex-col gap-0.5">
            <span className="text-muted-foreground line-through">{c.before.text}</span>
            <span className="text-foreground">→ {c.after.text}</span>
          </li>
        ))}
        {report.abstracted?.map((m) => (
          <li key={`ab-${m.id}`} className="text-green-600">
            + {m.text}
          </li>
        ))}
        {report.promoted?.map((m) => (
          <li key={`pr-${m.id}`} className="text-green-600">
            + {m.text}
          </li>
        ))}
        {report.deleted?.map((m) => (
          <li key={`del-${m.id}`} className="text-red-500 line-through">
            {m.text}
          </li>
        ))}
        {report.decayed?.map((m) => (
          <li key={`dec-${m.id}`} className="text-red-500/80 line-through">
            {m.text} <span className="not-line-through text-muted-foreground">(stale)</span>
          </li>
        ))}
      </ul>
    </div>
  );
}

function PreviewShell({
  children,
  onDismiss,
  action,
}: {
  children: React.ReactNode;
  onDismiss: () => void;
  action?: React.ReactNode;
}) {
  return (
    <div className="mb-3 rounded-md border bg-muted/30 p-3 flex items-center gap-2">
      {children}
      <div className="ml-auto flex items-center gap-1">
        {action}
        <Button size="sm" variant="ghost" onClick={onDismiss}>
          Dismiss
        </Button>
      </div>
    </div>
  );
}

function categoryColor(cat: string): string {
  switch (cat) {
    case "identity":
      return "bg-agent/15 text-agent";
    case "preference":
      return "bg-primary/15 text-primary";
    case "project":
      return "bg-blue-500/15 text-blue-500";
    default:
      return "bg-muted text-muted-foreground";
  }
}

// confidenceStyle maps a confidence tier to a muted dot + label. EXTRACTED (ground
// truth) is not shown — there's nothing to second-guess.
// confidenceChip is shown only for facts worth flagging — those in the confirm band
// (cross-project generalizations and AMBIGUOUS facts). Ordinary inferred facts (the
// 0.8 majority) get no chip, so the list stays quiet and the signal stays meaningful.
function confidenceChip(memory: Memory): { dot: string; label: string } | null {
  if (!needsConfirmation(memory)) return null;
  if (memory.confidence === "ambiguous") return { dot: "bg-amber-500", label: "unsure" };
  return { dot: "bg-muted-foreground/50", label: "unverified" };
}

function MemoryCard({ memory }: { memory: Memory }) {
  const { update, remove, pin, lock, confirm } = useBrainStore();
  const [editing, setEditing] = useState(false);
  const [text, setText] = useState(memory.text);
  const conf = confidenceChip(memory);
  const canConfirm = needsConfirmation(memory);

  const act = async (fn: () => Promise<unknown>, errMsg: string) => {
    try {
      await fn();
    } catch (err) {
      toast.error(getErrorMessage(err, errMsg));
    }
  };

  const saveEdit = async () => {
    const t = text.trim();
    if (!t || t === memory.text) {
      setEditing(false);
      return;
    }
    await act(() => update(memory.id, { text: t }), "Failed to update");
    setEditing(false);
  };

  return (
    <div className="rounded-md border bg-card/50 p-3 group">
      {editing ? (
        <div className="space-y-2">
          <Textarea value={text} onChange={(e) => setText(e.target.value)} rows={3} autoFocus />
          <div className="flex gap-2 justify-end">
            <Button
              size="sm"
              variant="ghost"
              onClick={() => {
                setText(memory.text);
                setEditing(false);
              }}
            >
              Cancel
            </Button>
            <Button size="sm" onClick={saveEdit}>
              Save
            </Button>
          </div>
        </div>
      ) : (
        <button
          type="button"
          className="text-sm text-left w-full whitespace-pre-wrap"
          onDoubleClick={() => {
            setText(memory.text);
            setEditing(true);
          }}
          title="Double-click to edit"
        >
          {memory.text}
        </button>
      )}

      <div className="flex items-center gap-1.5 mt-2 flex-wrap">
        <Badge className={categoryColor(memory.category)}>{memory.category}</Badge>
        <span className="text-[10px] text-muted-foreground">{memory.source}</span>
        {memory.uses > 0 && (
          <span className="text-[10px] text-muted-foreground tabular-nums">
            · used {memory.uses}×
          </span>
        )}
        {conf && (
          <span
            className="flex items-center gap-1 text-[10px] text-muted-foreground"
            title={`Confidence: ${memory.confidence} (${((memory.confidenceScore ?? 0) * 100).toFixed(0)}%)`}
          >
            <span className={`inline-block size-1.5 rounded-full ${conf.dot}`} />
            {conf.label}
          </span>
        )}
        {memory.locked && <Lock className="size-3 text-muted-foreground" aria-label="locked" />}

        <div className="ml-auto flex items-center gap-0.5 opacity-0 group-hover:opacity-100 transition-opacity">
          {canConfirm && (
            <IconBtn
              title="Confirm — keep as ground truth (exempt from consolidation)"
              onClick={() => act(() => confirm(memory.id), "Failed to confirm")}
            >
              <Check className="size-3.5" />
            </IconBtn>
          )}
          <IconBtn
            title={memory.pinned ? "Unpin" : "Pin (always injected)"}
            onClick={() => act(() => pin(memory.id, !memory.pinned), "Failed to pin")}
            active={memory.pinned}
          >
            {memory.pinned ? <Pin className="size-3.5" /> : <PinOff className="size-3.5" />}
          </IconBtn>
          <IconBtn
            title={
              memory.locked ? "Unlock (allow consolidation)" : "Lock (protect from consolidation)"
            }
            onClick={() => act(() => lock(memory.id, !memory.locked), "Failed to lock")}
            active={memory.locked}
          >
            {memory.locked ? <Lock className="size-3.5" /> : <LockOpen className="size-3.5" />}
          </IconBtn>
          <IconBtn title="Delete" onClick={() => act(() => remove(memory.id), "Failed to delete")}>
            <Trash2 className="size-3.5" />
          </IconBtn>
        </div>
      </div>
    </div>
  );
}

function IconBtn({
  children,
  title,
  onClick,
  active,
}: {
  children: React.ReactNode;
  title: string;
  onClick: () => void;
  active?: boolean;
}) {
  return (
    <button
      type="button"
      title={title}
      onClick={onClick}
      className={`size-6 rounded flex items-center justify-center transition-colors hover:bg-muted ${
        active ? "text-primary" : "text-muted-foreground hover:text-foreground"
      }`}
    >
      {children}
    </button>
  );
}

function AddMemoryForm({
  projects,
  onSubmit,
  onCancel,
}: {
  projects: { id: string; name: string }[];
  onSubmit: (input: { scope: string; text: string; category: string }) => void;
  onCancel: () => void;
}) {
  const [text, setText] = useState("");
  const [category, setCategory] = useState("fact");
  const [scope, setScope] = useState(GLOBAL_SCOPE);

  return (
    <div className="px-4 py-3 border-b bg-muted/20 space-y-2">
      <Textarea
        placeholder="A durable fact worth remembering…"
        value={text}
        onChange={(e) => setText(e.target.value)}
        rows={2}
        autoFocus
      />
      <div className="flex items-center gap-2">
        <select
          value={category}
          onChange={(e) => setCategory(e.target.value)}
          className="h-8 rounded-md border bg-background px-2 text-sm"
        >
          {CATEGORIES.map((c) => (
            <option key={c} value={c}>
              {c}
            </option>
          ))}
        </select>
        <select
          value={scope}
          onChange={(e) => setScope(e.target.value)}
          className="h-8 rounded-md border bg-background px-2 text-sm max-w-[12rem]"
        >
          <option value={GLOBAL_SCOPE}>Global</option>
          {projects.map((p) => (
            <option key={p.id} value={`project:${p.id}`}>
              {p.name}
            </option>
          ))}
        </select>
        <div className="ml-auto flex gap-2">
          <Button size="sm" variant="ghost" onClick={onCancel}>
            Cancel
          </Button>
          <Button
            size="sm"
            disabled={!text.trim()}
            onClick={() => onSubmit({ scope, text: text.trim(), category })}
          >
            Add
          </Button>
        </div>
      </div>
    </div>
  );
}
