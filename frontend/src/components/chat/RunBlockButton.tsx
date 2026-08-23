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
import { useLogicalProjects } from "~/hooks/useLogicalProjects";
import { openPrefilledNewSession } from "~/lib/session/new-session-draft";
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

  // One entry per repo, not per checkout (multi-machine): the same repo on two
  // machines is one target, and it lands on the representative's new-session
  // page where "Run on" picks the machine.
  const rows = useLogicalProjects();
  // "Current" project = the project of the active session, when there is one.
  const activeProjectId = useChatStore(
    useShallow((s) =>
      s.activeSessionId ? s.sessions[s.activeSessionId]?.meta.projectId : undefined,
    ),
  );

  const targets = useMemo<ProjectTarget[]>(() => {
    const mapped = rows.map((r) => ({ id: r.id, slug: r.slug, name: r.name }));
    if (!activeProjectId) return mapped;
    // Float the current repo to the top — matched through the group, so a
    // session running on a remote member still floats its logical row.
    const idx = rows.findIndex((r) => r.members.some((m) => m.projectId === activeProjectId));
    if (idx <= 0) return mapped;
    const [current] = mapped.splice(idx, 1);
    return current ? [current, ...mapped] : mapped;
  }, [rows, activeProjectId]);

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
