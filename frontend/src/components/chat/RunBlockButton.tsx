import { useNavigate } from "@tanstack/react-router";
import { ChevronDown, Play } from "lucide-react";
import { useMemo } from "react";
import { useShallow } from "zustand/shallow";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuTrigger,
} from "~/components/ui/dropdown-menu";
import { openPrefilledNewSession } from "~/lib/session/new-session-draft";
import { useAppStore } from "~/stores/app-store";
import { useChatStore } from "~/stores/chat-store";

interface ProjectTarget {
  id: string;
  slug: string;
  name: string;
}

/**
 * Runs a code block as a *new* session — never the current one (the block is
 * already in this session's context). Opens the target project's new-session view
 * with the block pre-filled into the composer, WITHOUT sending: the user edits and
 * commits, which is when the session is actually created (see openPrefilledNewSession).
 *
 * Targets: the current session's project first (when resolvable), then every other
 * project. With a single project it collapses to a one-click button; with none it
 * renders nothing.
 */
export function RunBlockButton({ code }: { code: string }) {
  const navigate = useNavigate();

  // Subscribe to the stable projects array (never a fresh .map() — Zustand
  // stable-ref rule) and derive the minimal target shape in render.
  const projects = useAppStore((s) => s.projects);
  // "Current" project = the project of the active session, when there is one.
  const activeProjectId = useChatStore(
    useShallow((s) =>
      s.activeSessionId ? s.sessions[s.activeSessionId]?.meta.projectId : undefined,
    ),
  );

  const targets = useMemo<ProjectTarget[]>(() => {
    const mapped = projects.map((p) => ({ id: p.id, slug: p.slug, name: p.name }));
    if (!activeProjectId) return mapped;
    // Float the current project to the top.
    const idx = mapped.findIndex((p) => p.id === activeProjectId);
    if (idx <= 0) return mapped;
    const [current] = mapped.splice(idx, 1);
    return current ? [current, ...mapped] : mapped;
  }, [projects, activeProjectId]);

  if (targets.length === 0 || !code.trim()) return null;

  const run = (t: ProjectTarget) =>
    openPrefilledNewSession(navigate, { projectId: t.id, projectSlug: t.slug, text: code });

  // Single project: one-click, no menu.
  if (targets.length === 1) {
    const only = targets[0];
    if (!only) return null;
    return (
      <button
        type="button"
        className="code-run-btn"
        onClick={() => run(only)}
        aria-label="Run as new session"
        title="Run as a new session"
      >
        <Play className="h-3 w-3" />
      </button>
    );
  }

  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <button type="button" className="code-run-btn" aria-label="Run as new session">
          <Play className="h-3 w-3" />
          <ChevronDown className="h-3 w-3" />
        </button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end">
        <DropdownMenuLabel className="text-xs text-muted-foreground">
          Run as new session in…
        </DropdownMenuLabel>
        {targets.map((t) => (
          <DropdownMenuItem key={t.id} onClick={() => run(t)} className="text-xs gap-2">
            {t.name}
            {t.id === activeProjectId && (
              <span className="ml-auto text-[10px] text-muted-foreground">current</span>
            )}
          </DropdownMenuItem>
        ))}
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
