import { createFileRoute } from "@tanstack/react-router";
import { ChatPanel } from "~/components/chat/ChatPanel";
import { StatusPage } from "~/components/layout/PageHeader";
import { useAppStore } from "~/stores/app-store";
import { useChatStore } from "~/stores/chat-store";

export const Route = createFileRoute("/project/$projectSlug/session/$sessionShortId")({
  component: SessionPage,
});

function SessionPage() {
  const { projectSlug, sessionShortId } = Route.useParams();
  const project = useAppStore((s) => s.projects.find((p) => p.slug === projectSlug));
  const sessionId = useChatStore((s) => {
    if (!project) return undefined;
    return Object.keys(s.sessions).find(
      (id) => s.sessions[id]?.meta.projectId === project.id && id.startsWith(sessionShortId),
    );
  });
  const sessionListLoaded = useChatStore((s) =>
    project ? s.loadedProjects.has(project.id) : false,
  );

  if (!project) return null;

  if (!sessionListLoaded) {
    return <StatusPage message="Loading..." />;
  }

  if (!sessionId) {
    return <StatusPage message="Session not found" />;
  }

  return <ChatPanel projectId={project.id} sessionId={sessionId} />;
}
