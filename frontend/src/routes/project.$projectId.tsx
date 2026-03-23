import { createFileRoute } from "@tanstack/react-router";
import { ChatPanel } from "~/components/chat/ChatPanel";
import { useKeyboardShortcuts } from "~/hooks/useKeyboardShortcuts";

export const Route = createFileRoute("/project/$projectId")({
	component: ProjectPage,
});

function ProjectPage() {
	const { projectId } = Route.useParams();
	useKeyboardShortcuts(projectId);
	return <ChatPanel projectId={projectId} />;
}
