/**
 * The sidebar's one "New" affordance: a compact primary button opening a
 * project palette (favorites first, fuzzy filter, keyboard nav). Picking a
 * project lands on that project's new-session page. Succeeds the retired
 * CombinedLauncher, trimmed to a single behavior.
 */
import { useNavigate } from "@tanstack/react-router";
import { Plus, Search, Star } from "lucide-react";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { toast } from "sonner";
import { ProjectPill } from "~/components/ui/project-pill";
import { useWebSocket } from "~/hooks/useWebSocket";
import { setProjectFavorite } from "~/lib/project-actions";
import { cn, getErrorMessage } from "~/lib/utils";
import { useAppStore } from "~/stores/app-store";

export function NewSessionButton() {
  const navigate = useNavigate();
  const ws = useWebSocket();
  const projects = useAppStore((s) => s.projects);
  const [open, setOpen] = useState(false);
  const [search, setSearch] = useState("");
  const [rawSelectedIdx, setSelectedIdx] = useState(0);
  const inputRef = useRef<HTMLInputElement>(null);
  const rootRef = useRef<HTMLDivElement>(null);

  const filtered = useMemo(() => {
    const q = search.trim().toLowerCase();
    const matches = q
      ? projects.filter((p) => p.name.toLowerCase().includes(q) || p.slug.toLowerCase().includes(q))
      : projects;
    return [...matches].sort((a, b) => {
      if (a.favorite !== b.favorite) return b.favorite - a.favorite;
      return a.name.localeCompare(b.name);
    });
  }, [projects, search]);

  const selectedIdx = filtered.length === 0 ? 0 : Math.min(rawSelectedIdx, filtered.length - 1);

  useEffect(() => {
    if (!open) return;
    inputRef.current?.focus();
    setSearch("");
    setSelectedIdx(0);
    const handler = (e: MouseEvent) => {
      if (rootRef.current && !rootRef.current.contains(e.target as Node)) setOpen(false);
    };
    document.addEventListener("mousedown", handler);
    return () => document.removeEventListener("mousedown", handler);
  }, [open]);

  const launch = useCallback(
    (slug: string) => {
      setOpen(false);
      useAppStore.getState().setSidebarOpen(false);
      navigate({ to: "/project/$projectSlug/session/new", params: { projectSlug: slug } });
    },
    [navigate],
  );

  const handleKeyDown = useCallback(
    (e: React.KeyboardEvent) => {
      if (e.key === "ArrowDown") {
        e.preventDefault();
        setSelectedIdx((i) => Math.min(i + 1, filtered.length - 1));
      } else if (e.key === "ArrowUp") {
        e.preventDefault();
        setSelectedIdx((i) => Math.max(i - 1, 0));
      } else if (e.key === "Enter") {
        e.preventDefault();
        const project = filtered[selectedIdx];
        if (project) launch(project.slug);
      } else if (e.key === "Escape") {
        setOpen(false);
      }
    },
    [filtered, selectedIdx, launch],
  );

  if (projects.length === 0) return null;

  return (
    <div className="relative" ref={rootRef}>
      <button
        type="button"
        onClick={() => setOpen((o) => !o)}
        aria-expanded={open}
        className={cn(
          "flex cursor-pointer items-center gap-1 rounded-md bg-primary px-2.5 py-1 text-xs font-semibold text-primary-foreground",
          "transition-opacity hover:opacity-90 focus-visible:outline-2 focus-visible:outline-ring/50",
          "max-md:min-h-8",
        )}
      >
        <Plus className="size-3.5" />
        New
      </button>

      {open && (
        <div className="absolute right-0 top-full z-50 mt-1 w-64 overflow-hidden rounded-md border border-sidebar-border bg-sidebar shadow-xl">
          <div className="flex items-center gap-2 border-b border-sidebar-border/50 px-3 py-2">
            <Search className="size-4 shrink-0 text-muted-foreground-faint" />
            <input
              ref={inputRef}
              type="text"
              placeholder="Start a session in…"
              value={search}
              onChange={(e) => setSearch(e.target.value)}
              onKeyDown={handleKeyDown}
              className="flex-1 bg-transparent text-sm text-sidebar-foreground outline-none placeholder:text-muted-foreground-faint"
            />
          </div>
          <div className="max-h-64 overflow-y-auto py-1">
            {filtered.map((p, i) => (
              <div
                key={p.id}
                onMouseEnter={() => setSelectedIdx(i)}
                className={cn(
                  "flex w-full items-center gap-3 px-3 py-2",
                  i === selectedIdx ? "bg-sidebar-accent" : "hover:bg-sidebar-accent/50",
                )}
              >
                <button
                  type="button"
                  onClick={() => launch(p.slug)}
                  className="flex min-w-0 flex-1 cursor-pointer items-center text-left"
                >
                  <ProjectPill slug={p.slug} showIcon size="md" background={false} />
                </button>
                <button
                  type="button"
                  aria-label={p.favorite === 1 ? "Unfavorite" : "Favorite"}
                  onClick={() => {
                    setProjectFavorite(ws, p.id, p.favorite !== 1).catch((err) =>
                      toast.error(getErrorMessage(err, "Failed to update favorite")),
                    );
                  }}
                  className="shrink-0 cursor-pointer p-1 transition-colors hover:text-yellow-400"
                >
                  <Star
                    className={cn(
                      "size-3.5",
                      p.favorite === 1
                        ? "fill-yellow-400 text-yellow-400"
                        : "text-muted-foreground-faint",
                    )}
                  />
                </button>
              </div>
            ))}
            {filtered.length === 0 && (
              <div className="px-3 py-2.5 text-sm text-muted-foreground-faint">
                No matching projects
              </div>
            )}
          </div>
        </div>
      )}
    </div>
  );
}
