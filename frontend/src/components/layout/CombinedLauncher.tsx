import { useNavigate, useParams } from "@tanstack/react-router";
import { ChevronUp, Plus, Search, Star } from "lucide-react";
import { memo, useCallback, useEffect, useMemo, useRef, useState } from "react";
import { toast } from "sonner";

import { ProjectPill } from "~/components/ui/project-pill";
import { useTheme } from "~/hooks/useTheme";
import { useWebSocket } from "~/hooks/useWebSocket";
import { setProjectFavorite } from "~/lib/project-actions";
import { getProjectColor } from "~/lib/project-colors";
import { cn, getErrorMessage } from "~/lib/utils";
import { useAppStore } from "~/stores/app-store";

interface CombinedLauncherProps {
  selectedProjectId?: string | null;
}

export const CombinedLauncher = memo(function CombinedLauncher({
  selectedProjectId = null,
}: CombinedLauncherProps) {
  const navigate = useNavigate();
  const ws = useWebSocket();
  const { resolvedTheme } = useTheme();
  const params = useParams({ strict: false }) as { projectSlug?: string };
  const projects = useAppStore((s) => s.projects);
  const [paletteOpen, setPaletteOpen] = useState(false);
  const [hovered, setHovered] = useState(false);
  const [active, setActive] = useState(false);
  const [search, setSearch] = useState("");
  const [sortSnapshot, setSortSnapshot] = useState<string[]>([]);
  const inputRef = useRef<HTMLInputElement>(null);
  const paletteRef = useRef<HTMLDivElement>(null);

  const projectIds = useMemo(() => projects.map((p) => p.id), [projects]);

  // Default project: sidebar selection → route's project → favorite → first
  const defaultProject = useMemo(() => {
    if (selectedProjectId) {
      const found = projects.find((p) => p.id === selectedProjectId);
      if (found) return found;
    }
    if (params.projectSlug) {
      const found = projects.find((p) => p.slug === params.projectSlug);
      if (found) return found;
    }
    const sorted = [...projects].sort((a, b) => {
      if (a.favorite !== b.favorite) return b.favorite - a.favorite;
      return a.name.localeCompare(b.name);
    });
    return sorted[0];
  }, [projects, params.projectSlug, selectedProjectId]);

  const openPalette = useCallback(() => {
    const current = useAppStore.getState().projects;
    setSortSnapshot(
      [...current]
        .sort((a, b) => {
          if (a.favorite !== b.favorite) return b.favorite - a.favorite;
          return a.name.localeCompare(b.name);
        })
        .map((p) => p.id),
    );
    setPaletteOpen(true);
  }, []);

  const filteredProjects = useMemo(() => {
    if (!search) {
      const orderMap = new Map(sortSnapshot.map((id, i) => [id, i]));
      return [...projects].sort((a, b) => {
        const aIdx = orderMap.get(a.id) ?? Number.POSITIVE_INFINITY;
        const bIdx = orderMap.get(b.id) ?? Number.POSITIVE_INFINITY;
        return aIdx - bIdx;
      });
    }
    const q = search.toLowerCase();
    return projects
      .filter((p) => p.name.toLowerCase().includes(q) || p.slug.toLowerCase().includes(q))
      .sort((a, b) => a.name.localeCompare(b.name));
  }, [projects, search, sortSnapshot]);

  const [rawSelectedIdx, setSelectedIdx] = useState(0);
  const selectedIdx =
    filteredProjects.length === 0 ? 0 : Math.min(rawSelectedIdx, filteredProjects.length - 1);

  // Focus input and reset state when palette opens
  useEffect(() => {
    if (paletteOpen) {
      inputRef.current?.focus();
      setSearch("");
      setSelectedIdx(0);
    }
  }, [paletteOpen]);

  // Close on outside click
  useEffect(() => {
    if (!paletteOpen) return;
    const handler = (e: MouseEvent) => {
      if (paletteRef.current && !paletteRef.current.contains(e.target as Node)) {
        setPaletteOpen(false);
      }
    };
    document.addEventListener("mousedown", handler);
    return () => document.removeEventListener("mousedown", handler);
  }, [paletteOpen]);

  const launchInProject = useCallback(
    (slug: string) => {
      useAppStore.getState().setSidebarOpen(false);
      navigate({ to: "/project/$projectSlug/session/new", params: { projectSlug: slug } });
      setPaletteOpen(false);
    },
    [navigate],
  );

  const handleMainClick = useCallback(() => {
    if (!defaultProject) return;
    launchInProject(defaultProject.slug);
  }, [defaultProject, launchInProject]);

  const handleKeyDown = useCallback(
    (e: React.KeyboardEvent) => {
      if (e.key === "ArrowDown") {
        e.preventDefault();
        setSelectedIdx((i) => Math.min(i + 1, filteredProjects.length - 1));
      } else if (e.key === "ArrowUp") {
        e.preventDefault();
        setSelectedIdx((i) => Math.max(i - 1, 0));
      } else if (e.key === "Enter") {
        e.preventDefault();
        const project = filteredProjects[selectedIdx];
        if (project) launchInProject(project.slug);
      } else if (e.key === "Escape") {
        setPaletteOpen(false);
      }
    },
    [filteredProjects, selectedIdx, launchInProject],
  );

  if (projects.length === 0) return null;

  return (
    <div className="relative px-2 pb-2" ref={paletteRef}>
      {/* Palette overlay */}
      {paletteOpen && (
        <div className="absolute bottom-full left-0 right-0 mb-1 rounded-md border border-sidebar-border bg-sidebar shadow-xl overflow-hidden">
          <div className="flex items-center gap-2 border-b border-sidebar-border/50 px-3 py-2">
            <Search className="size-4 shrink-0 text-muted-foreground-faint" />
            <input
              ref={inputRef}
              type="text"
              placeholder="Search projects..."
              value={search}
              onChange={(e) => setSearch(e.target.value)}
              onKeyDown={handleKeyDown}
              className="flex-1 bg-transparent text-sm text-sidebar-foreground placeholder:text-muted-foreground-faint outline-none"
            />
          </div>
          <div className="max-h-64 overflow-y-auto py-1">
            {filteredProjects.map((p, i) => (
              <div
                key={p.id}
                onMouseEnter={() => setSelectedIdx(i)}
                className={cn(
                  "flex w-full items-center gap-3 px-3 py-2.5",
                  i === selectedIdx ? "bg-sidebar-accent" : "hover:bg-sidebar-accent/50",
                )}
              >
                <button
                  type="button"
                  onClick={() => launchInProject(p.slug)}
                  className="flex flex-1 items-center min-w-0 text-left cursor-pointer"
                >
                  <ProjectPill slug={p.slug} showIcon size="md" background={false} />
                </button>
                <button
                  type="button"
                  onClick={(e) => {
                    e.stopPropagation();
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
            {filteredProjects.length === 0 && (
              <div className="px-3 py-2.5 text-sm text-muted-foreground-faint">
                No matching projects
              </div>
            )}
          </div>
        </div>
      )}

      {/* Split button */}
      {(() => {
        const color = defaultProject
          ? getProjectColor(defaultProject.color, defaultProject.id, projectIds, resolvedTheme)
          : null;
        return (
          <div
            onMouseEnter={() => setHovered(true)}
            onMouseLeave={() => setHovered(false)}
            onPointerDown={() => setActive(true)}
            onAnimationEnd={() => setActive(false)}
            className={cn("shimmer-btn flex rounded-md border", active && "shimmer-active")}
            style={{
              borderColor: color ? (active ? color.bg : `${color.bg}80`) : undefined,
              backgroundColor: color
                ? `color-mix(in srgb, ${color.bg} ${hovered ? "15%" : "8%"}, var(--sidebar))`
                : undefined,
              boxShadow: active && color ? `0 0 8px ${color.bg}60` : undefined,
              transition:
                "border-color 0.4s ease, box-shadow 0.4s ease, background-color 0.15s ease",
            }}
          >
            <button
              type="button"
              onClick={handleMainClick}
              className="flex flex-1 items-center justify-center gap-1.5 rounded-l-md px-3 py-2 text-sm cursor-pointer focus-visible:outline-none focus-visible:ring-2"
              style={{ color: color?.fg, "--tw-ring-color": color?.fg } as React.CSSProperties}
            >
              {defaultProject ? (
                <ProjectPill
                  slug={defaultProject.slug}
                  showIcon
                  size="md"
                  background={false}
                  className="truncate"
                />
              ) : (
                <>
                  <Plus className="size-3.5" />
                  <span className="truncate font-medium">New session</span>
                </>
              )}
            </button>
            <button
              type="button"
              onClick={() => (paletteOpen ? setPaletteOpen(false) : openPalette())}
              className={cn(
                "flex items-center justify-center rounded-r-md border-l border-white/10 px-2 py-2 cursor-pointer focus-visible:outline-none focus-visible:ring-2",
                paletteOpen && "bg-white/10",
              )}
              style={{ color: color?.fg, "--tw-ring-color": color?.fg } as React.CSSProperties}
            >
              <ChevronUp className="size-3.5" />
            </button>
          </div>
        );
      })()}
    </div>
  );
});
