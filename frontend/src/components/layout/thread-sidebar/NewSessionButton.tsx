/**
 * The sidebar's one "New" affordance: a compact primary button opening a
 * project palette (favorites first, fuzzy filter, keyboard nav). Picking a
 * project lands on that project's new-session page. Succeeds the retired
 * CombinedLauncher, trimmed to a single behavior.
 *
 * Since the flat sidebar dropped project rows, this palette is also the only
 * per-project surface left — so each row carries what those rows owned: the
 * remote sync pills (push/pull) and a jump to project settings.
 *
 * Positioning goes through Radix Popover: the panel is wider than the space
 * left of the trigger inside a 288px sidebar, so it needs real collision
 * handling rather than a hand-rolled `absolute right-0` (which pushed it off
 * the left edge of the screen).
 */
import { useNavigate } from "@tanstack/react-router";
import { Circle, Plus, Search, Settings, Star } from "lucide-react";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { toast } from "sonner";
import { ProjectGitPill } from "~/components/layout/git/ProjectGitPill";
import { Popover, PopoverContent, PopoverTrigger } from "~/components/ui/popover";
import { ProjectPill } from "~/components/ui/project-pill";
import { useWebSocket } from "~/hooks/useWebSocket";
import { setProjectFavorite } from "~/lib/project-actions";
import { cn, getErrorMessage } from "~/lib/utils";
import { useAppStore } from "~/stores/app-store";
import { useMachineStore } from "~/stores/machine-store";

export function NewSessionButton() {
  const navigate = useNavigate();
  const projects = useAppStore((s) => s.projects);
  const machineStatuses = useMachineStore((s) => s.statuses);
  const [open, setOpen] = useState(false);
  const [search, setSearch] = useState("");
  const [rawSelectedIdx, setSelectedIdx] = useState(0);
  const inputRef = useRef<HTMLInputElement>(null);

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
    setSearch("");
    setSelectedIdx(0);
  }, [open]);

  const go = useCallback(
    (to: "new" | "settings" | "repo", slug: string) => {
      setOpen(false);
      useAppStore.getState().setSidebarOpen(false);
      if (to === "settings") {
        navigate({ to: "/project/$projectSlug/settings", params: { projectSlug: slug } });
        return;
      }
      if (to === "repo") {
        navigate({ to: "/project/$projectSlug", params: { projectSlug: slug } });
        return;
      }
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
        // Keyboard launch obeys the same rule the row does: a machine that is
        // away can't take a new session.
        if (!project) return;
        if (project.machineId && machineStatuses[project.machineId] !== "connected") return;
        go("new", project.slug);
      }
    },
    [filtered, selectedIdx, go, machineStatuses],
  );

  if (projects.length === 0) return null;

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger asChild>
        <button
          type="button"
          className={cn(
            "flex cursor-pointer items-center gap-1 rounded-md bg-primary px-2.5 py-1 text-xs font-semibold text-primary-foreground",
            "transition-opacity hover:opacity-90 focus-visible:outline-2 focus-visible:outline-ring/50",
            "max-md:min-h-8",
          )}
        >
          <Plus className="size-3.5" />
          New
        </button>
      </PopoverTrigger>

      <PopoverContent
        align="end"
        collisionPadding={8}
        onOpenAutoFocus={(e) => {
          e.preventDefault();
          inputRef.current?.focus();
        }}
        className="w-80 max-w-[calc(100vw-16px)] overflow-hidden border-sidebar-border bg-sidebar p-0 shadow-xl"
      >
        <div className="flex items-center gap-2 border-b border-sidebar-border/50 px-3 py-2">
          <Search className="size-4 shrink-0 text-muted-foreground-faint" />
          <input
            ref={inputRef}
            type="text"
            placeholder="Start a session in…"
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            onKeyDown={handleKeyDown}
            className="min-w-0 flex-1 bg-transparent text-sm text-sidebar-foreground outline-none placeholder:text-muted-foreground-faint"
          />
        </div>
        <div className="max-h-72 overflow-y-auto py-1">
          {filtered.map((p, i) => (
            <ProjectPaletteRow
              key={p.id}
              project={p}
              active={i === selectedIdx}
              onHover={() => setSelectedIdx(i)}
              onLaunch={() => go("new", p.slug)}
              onSettings={() => go("settings", p.slug)}
              onOpenRepo={() => go("repo", p.slug)}
            />
          ))}
          {filtered.length === 0 && (
            <div className="px-3 py-2.5 text-sm text-muted-foreground-faint">
              No matching projects
            </div>
          )}
        </div>
      </PopoverContent>
    </Popover>
  );
}

interface PaletteProject {
  id: string;
  slug: string;
  favorite: number;
  machineId?: string;
}

function ProjectPaletteRow({
  project,
  active,
  onHover,
  onLaunch,
  onSettings,
  onOpenRepo,
}: {
  project: PaletteProject;
  active: boolean;
  onHover: () => void;
  onLaunch: () => void;
  onSettings: () => void;
  onOpenRepo: () => void;
}) {
  const ws = useWebSocket();
  const gitStatus = useAppStore((s) => s.projectGitStatus[project.id]);
  const machine = useMachineStore((s) =>
    project.machineId ? (s.machines[project.machineId] ?? null) : null,
  );
  const machineStatus = useMachineStore((s) =>
    project.machineId ? (s.statuses[project.machineId] ?? "disconnected") : "connected",
  );
  // The project stays listed — knowing where a repo lives is worth a row —
  // but a session can't be started on a machine that is away.
  const away = !!project.machineId && machineStatus !== "connected";
  const favorite = project.favorite === 1;
  const dirty = gitStatus?.uncommittedCount ?? 0;

  return (
    <div
      onMouseEnter={onHover}
      className={cn(
        "flex w-full items-center gap-1.5 px-3 py-2",
        active ? "bg-sidebar-accent" : "hover:bg-sidebar-accent/50",
      )}
    >
      <button
        type="button"
        onClick={onLaunch}
        disabled={away}
        title={away ? `${machine?.label ?? "That machine"} is offline` : undefined}
        className={cn(
          "flex min-w-0 flex-1 items-center gap-2 text-left",
          away ? "cursor-not-allowed opacity-45" : "cursor-pointer",
        )}
      >
        <ProjectPill slug={project.slug} showIcon size="md" background={false} />
        {away && (
          <span className="shrink-0 font-mono text-[9.5px] text-muted-foreground-faint">
            offline
          </span>
        )}
      </button>

      {/* Uncommitted work on the project's own checkout — the only route back
          to the project git panel now that project rows are gone. */}
      {dirty > 0 && (
        <button
          type="button"
          onClick={onOpenRepo}
          title={`${dirty} uncommitted file${dirty === 1 ? "" : "s"} — open project git`}
          className="inline-flex shrink-0 cursor-pointer items-center gap-0.5 rounded-full bg-warning/15 px-1.5 py-0.5 text-[10px] font-medium text-warning transition-colors hover:bg-warning/25"
        >
          <Circle className="size-2 fill-current" />
          {dirty}
        </button>
      )}

      <ProjectGitPill
        projectId={project.id}
        projectSlug={project.slug}
        gitStatus={gitStatus}
        className="mr-0.5"
      />

      <button
        type="button"
        aria-label="Project settings"
        title="Project settings"
        onClick={onSettings}
        className={cn(
          "shrink-0 cursor-pointer p-1 text-muted-foreground-faint transition-colors hover:text-foreground",
          active ? "opacity-100" : "opacity-0 max-md:opacity-100",
        )}
      >
        <Settings className="size-3.5" />
      </button>
      <button
        type="button"
        aria-label={favorite ? "Unfavorite" : "Favorite"}
        onClick={() => {
          setProjectFavorite(ws, project.id, !favorite).catch((err) =>
            toast.error(getErrorMessage(err, "Failed to update favorite")),
          );
        }}
        className="shrink-0 cursor-pointer p-1 transition-colors hover:text-yellow-400"
      >
        <Star
          className={cn(
            "size-3.5",
            favorite ? "fill-yellow-400 text-yellow-400" : "text-muted-foreground-faint",
          )}
        />
      </button>
    </div>
  );
}
