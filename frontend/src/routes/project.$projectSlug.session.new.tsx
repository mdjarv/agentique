import { createFileRoute } from "@tanstack/react-router";
import { NewChatPanel } from "~/components/chat/NewChatPanel";
import { useAppStore } from "~/stores/app-store";

export const Route = createFileRoute("/project/$projectSlug/session/new")({
  component: NewChatPage,
});

function NewChatPage() {
  const { projectSlug } = Route.useParams();
  const project = useAppStore((s) => s.projects.find((p) => p.slug === projectSlug));
  if (!project) return null;
  return <NewChatPanel projectId={project.id} projectSlug={projectSlug} />;
}
