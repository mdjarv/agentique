import { createFileRoute } from "@tanstack/react-router";
import { NewChatPanel } from "~/components/chat/NewChatPanel";

export const Route = createFileRoute("/project/$projectId/session/new")({
  component: NewChatPage,
});

function NewChatPage() {
  const { projectId } = Route.useParams();
  return <NewChatPanel projectId={projectId} />;
}
