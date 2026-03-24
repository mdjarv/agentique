import { createFileRoute } from "@tanstack/react-router";
import { ChatPanel } from "~/components/chat/ChatPanel";

export const Route = createFileRoute("/project/$projectId/session/$sessionId")({
  component: SessionPage,
});

function SessionPage() {
  const { projectId, sessionId } = Route.useParams();
  return <ChatPanel projectId={projectId} sessionId={sessionId} />;
}
