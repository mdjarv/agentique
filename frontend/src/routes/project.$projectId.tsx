import { createFileRoute } from "@tanstack/react-router";
import { ChatPanel } from "~/components/chat/ChatPanel";
import { useKeyboardShortcuts } from "~/hooks/useKeyboardShortcuts";

interface ProjectSearch {
  session?: string;
}

export const Route = createFileRoute("/project/$projectId")({
  component: ProjectPage,
  validateSearch: (search: Record<string, unknown>): ProjectSearch => ({
    session: typeof search.session === "string" ? search.session : undefined,
  }),
});

function ProjectPage() {
  const { projectId } = Route.useParams();
  const { session } = Route.useSearch();
  useKeyboardShortcuts(projectId);
  return <ChatPanel projectId={projectId} initialSessionId={session} />;
}
