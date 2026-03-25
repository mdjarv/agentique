import { createFileRoute } from "@tanstack/react-router";
import { ChatPanel } from "~/components/chat/ChatPanel";
import { useAppStore } from "~/stores/app-store";
import { useChatStore } from "~/stores/chat-store";

export const Route = createFileRoute("/project/$projectSlug/session/$sessionShortId")({
  component: SessionPage,
});

function SessionPage() {
  const { projectSlug, sessionShortId } = Route.useParams();
  const projectsLoaded = useAppStore((s) => s.projectsLoaded);
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

  if (!projectsLoaded || (project && !sessionListLoaded)) {
    return (
      <div className="flex flex-col h-full items-center justify-center text-muted-foreground">
        <p className="text-sm">Loading...</p>
      </div>
    );
  }

  if (!project) {
    return (
      <div className="flex flex-col h-full items-center justify-center text-muted-foreground">
        <p className="text-sm">Project not found</p>
      </div>
    );
  }

  if (!sessionId) {
    return (
      <div className="flex flex-col h-full items-center justify-center text-muted-foreground">
        <p className="text-sm">Session not found</p>
      </div>
    );
  }

  return <ChatPanel key={sessionShortId} projectId={project.id} sessionId={sessionId} />;
}
