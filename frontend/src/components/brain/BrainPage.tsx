import { Brain, Lock, LockOpen, Pin, PinOff, Plus, Sparkles, Trash2 } from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import { toast } from "sonner";
import { PageHeader } from "~/components/layout/PageHeader";
import { Badge } from "~/components/ui/badge";
import { Button } from "~/components/ui/button";
import { Input } from "~/components/ui/input";
import { Textarea } from "~/components/ui/textarea";
import type { Memory } from "~/lib/brain-api";
import { getErrorMessage } from "~/lib/utils";
import { useAppStore } from "~/stores/app-store";
import { useBrainStore } from "~/stores/brain-store";

const CATEGORIES = ["fact", "identity", "preference", "contact", "project", "goal", "task"];
const GLOBAL_SCOPE = "global";

export function BrainPage() {
  const { memories, semantic, loaded, load, create, consolidate } = useBrainStore();
  const projects = useAppStore((s) => s.projects);
  const [filter, setFilter] = useState("");
  const [adding, setAdding] = useState(false);

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

  const handleConsolidate = async (scope: string) => {
    try {
      await consolidate(scope);
      toast.success(`Consolidated ${labelForScope(scope)}`);
    } catch (err) {
      toast.error(getErrorMessage(err, "Consolidation failed"));
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
        {groups.map((g) => (
          <section key={g.scope}>
            <div className="flex items-center gap-2 mb-2">
              <h2 className="text-sm font-semibold">{labelForScope(g.scope)}</h2>
              <span className="text-xs text-muted-foreground tabular-nums">{g.items.length}</span>
              <Button
                size="sm"
                variant="ghost"
                className="ml-auto text-xs"
                onClick={() => handleConsolidate(g.scope)}
                title="Merge duplicates, distill captures, decay stale facts"
              >
                <Sparkles className="size-3.5" /> Tidy
              </Button>
            </div>
            <div className="space-y-2">
              {g.items.map((m) => (
                <MemoryCard key={m.id} memory={m} />
              ))}
            </div>
          </section>
        ))}
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
          onDoubleClick={() => setEditing(true)}
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
