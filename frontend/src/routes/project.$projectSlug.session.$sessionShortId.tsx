import { createFileRoute, useNavigate } from "@tanstack/react-router";
import { useCallback } from "react";
import { z } from "zod";
import { ChatPanel } from "~/components/chat/ChatPanel";
import { StatusPage } from "~/components/layout/PageHeader";
import { type DockView, legacyTabToDock } from "~/lib/session/dock";
import { useAppStore } from "~/stores/app-store";
import { useChatStore } from "~/stores/chat-store";

const searchSchema = z.object({
  /** Which dock view to open. Absent means "leave the dock as the user left it". */
  dock: z.enum(["work", "changes", "loops", "browser"]).optional(),
  /**
   * Pre-dock links, still minted by older peers and still sitting in people's
   * clipboards. Read-only: we never write it back, and `legacyTabToDock` owns
   * the mapping so the next rename has one place to look.
   */
  tab: z.enum(["chat", "todos", "git", "changes", "agents", "loops"]).optional(),
  /** Deep-link target: persisted turn index to scroll to (run "view turn"). */
  turn: z.coerce.number().int().nonnegative().optional(),
});

export const Route = createFileRoute("/project/$projectSlug/session/$sessionShortId")({
  component: SessionPage,
  validateSearch: searchSchema,
});

function SessionPage() {
  const { projectSlug, sessionShortId } = Route.useParams();
  const { dock, tab, turn } = Route.useSearch();
  const navigate = useNavigate();
  const project = useAppStore((s) => s.projects.find((p) => p.slug === projectSlug));
  const projectId = project?.id;
  const sessionId = useChatStore((s) => {
    if (!projectId) return undefined;
    // Prefix match on session ID — activeSessionId is often already set,
    // so check it first as a fast path before scanning all sessions.
    if (s.activeSessionId?.startsWith(sessionShortId)) return s.activeSessionId;
    for (const id in s.sessions) {
      if (id.startsWith(sessionShortId) && s.sessions[id]?.meta.projectId === projectId) return id;
    }
    return undefined;
  });
  const sessionListLoaded = useChatStore((s) =>
    project ? s.loadedProjects.has(project.id) : false,
  );

  const handleDockChange = useCallback(
    (view: DockView | null) => {
      navigate({
        to: "/project/$projectSlug/session/$sessionShortId",
        params: { projectSlug, sessionShortId },
        // Closing clears the param rather than spelling out "closed": the dock
        // remembers its own state per session, and an empty URL means "as I
        // left it" rather than "shut it".
        search: view ? { dock: view } : {},
        replace: true,
      });
    },
    [navigate, projectSlug, sessionShortId],
  );

  if (!project) return null;

  if (!sessionListLoaded) {
    return <StatusPage message="Loading..." />;
  }

  if (!sessionId) {
    return <StatusPage message="Session not found" />;
  }

  return (
    <ChatPanel
      projectId={project.id}
      sessionId={sessionId}
      dock={dock ?? legacyTabToDock(tab) ?? undefined}
      targetTurn={turn}
      onDockChange={handleDockChange}
    />
  );
}
