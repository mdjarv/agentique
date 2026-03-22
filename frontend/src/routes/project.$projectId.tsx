import { createFileRoute } from "@tanstack/react-router";
import { ChatPanel } from "~/components/chat/ChatPanel";

export const Route = createFileRoute("/project/$projectId")({
  component: ProjectPage,
});

function ProjectPage() {
  const { projectId } = Route.useParams();
  return <ChatPanel projectId={projectId} />;
}
