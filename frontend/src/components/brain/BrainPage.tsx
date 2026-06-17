import {
  ArrowUpToLine,
  Brain,
  ChevronRight,
  Loader2,
  Lock,
  LockOpen,
  Pin,
  PinOff,
  Plus,
  Sparkles,
  Trash2,
} from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import { toast } from "sonner";
import { PageHeader } from "~/components/layout/PageHeader";
import { Badge } from "~/components/ui/badge";
import { Button } from "~/components/ui/button";
import { Input } from "~/components/ui/input";
import { Textarea } from "~/components/ui/textarea";
import type { ConsolidateReport, Memory } from "~/lib/brain-api";
import { getErrorMessage } from "~/lib/utils";
import { useAppStore } from "~/stores/app-store";
import { useBrainStore } from "~/stores/brain-store";

const CATEGORIES = ["fact", "identity", "preference", "contact", "project", "goal", "task"];
const GLOBAL_SCOPE = "global";
const MODELS = ["opus", "sonnet", "haiku"];

export function BrainPage() {
  const {
    memories,
    semantic,
    loaded,
    load,
    create,
    preview,
    previewScope,
    previewing,
    applying,
    startPreview,
    applyPreview,
    dismissPreview,
    globalPreview,
    globalPreviewing,
    globalApplying,
    startGlobalPreview,
    applyGlobalPreview,
    dismissGlobalPreview,
  } = useBrainStore();
  const projects = useAppStore((s) => s.projects);
  const [filter, setFilter] = useState("");
  const [adding, setAdding] = useState(false);
  const [model, setModel] = useState("opus");
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
  }, [loaded, load]);

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

  const handleTidy = async (scope: string) => {
    try {
      await startPreview(scope, model);
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
      await startGlobalPreview(model);
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
        <select
          value={model}
          onChange={(e) => setModel(e.target.value)}
          className="h-8 rounded-md border bg-background px-2 text-xs capitalize"
          title="Model used to reorganize when you Tidy a scope"
        >
          {MODELS.map((m) => (
            <option key={m} value={m} className="capitalize">
              {m}
            </option>
          ))}
        </select>
        <Button size="sm" variant="outline" onClick={() => setAdding((v) => !v)}>
          <Plus className="size-4" /> Add
        </Button>
      </PageHeader>

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

      <div className="flex-1 overflow-y-auto p-4 space-y-6">
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
                      {globalPreviewing ? "Scanning…" : "Consolidate"}
                    </Button>
                  )}
                  <Button
                    size="sm"
                    variant="ghost"
                    className="text-xs"
                    disabled={previewing && isPreviewScope}
                    onClick={() => handleTidy(g.scope)}
                    title={`Preview a tidy with ${model}: merge duplicates, distill captures, decay stale facts`}
                  >
                    <Sparkles className="size-3.5" />
                    {previewing && isPreviewScope ? "Previewing…" : "Tidy"}
                  </Button>
                </div>
              </div>
              {isGlobal && (globalPreviewing || globalPreview) && (
                <ConsolidatePreview
                  previewing={globalPreviewing}
                  applying={globalApplying}
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
                  report={preview?.report ?? null}
                  onApply={handleApply}
                  onDismiss={dismissPreview}
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
  report,
  onApply,
  onDismiss,
  emptyLabel = "Already tidy — nothing to change.",
}: {
  previewing: boolean;
  applying: boolean;
  report: ConsolidateReport | null;
  onApply: () => void;
  onDismiss: () => void;
  emptyLabel?: string;
}) {
  if (previewing) {
    return (
      <div className="mb-3 rounded-md border bg-muted/30 p-3 flex items-center gap-2 text-xs text-muted-foreground">
        <Loader2 className="size-3.5 animate-spin" /> Analyzing memories…
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
          Skipped — this tidy would remove more than half of the scope (safety limit).
        </span>
      </PreviewShell>
    );
  }
  if (report.skipped || changes === 0) {
    return (
      <PreviewShell onDismiss={onDismiss}>
        <span className="text-xs text-muted-foreground">{emptyLabel}</span>
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
}: {
  children: React.ReactNode;
  onDismiss: () => void;
}) {
  return (
    <div className="mb-3 rounded-md border bg-muted/30 p-3 flex items-center gap-2">
      {children}
      <Button size="sm" variant="ghost" className="ml-auto" onClick={onDismiss}>
        Dismiss
      </Button>
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

function MemoryCard({ memory }: { memory: Memory }) {
  const { update, remove, pin, lock } = useBrainStore();
  const [editing, setEditing] = useState(false);
  const [text, setText] = useState(memory.text);

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
        {memory.locked && <Lock className="size-3 text-muted-foreground" aria-label="locked" />}

        <div className="ml-auto flex items-center gap-0.5 opacity-0 group-hover:opacity-100 transition-opacity">
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
