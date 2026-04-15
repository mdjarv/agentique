import { createFileRoute, useNavigate } from "@tanstack/react-router";
import { useCallback } from "react";
import { z } from "zod";
import { ChatPanel, type SessionTab } from "~/components/chat/ChatPanel";
import { StatusPage } from "~/components/layout/PageHeader";
import { useAppStore } from "~/stores/app-store";
import { useChatStore } from "~/stores/chat-store";

const searchSchema = z.object({
  tab: z.enum(["chat", "todos", "git", "changes"]).optional(),
});

export const Route = createFileRoute("/project/$projectSlug/session/$sessionShortId")({
  component: SessionPage,
  validateSearch: searchSchema,
});

function SessionPage() {
  const { projectSlug, sessionShortId } = Route.useParams();
  const { tab } = Route.useSearch();
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

  const handleTabChange = useCallback(
    (t: SessionTab) => {
      navigate({
        to: "/project/$projectSlug/session/$sessionShortId",
        params: { projectSlug, sessionShortId },
        search: t === "chat" ? {} : { tab: t },
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
      tab={tab}
      onTabChange={handleTabChange}
    />
  );
}
