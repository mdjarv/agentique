import { createFileRoute } from "@tanstack/react-router";
import { NewChatPanel } from "~/components/chat/NewChatPanel";
import { PageHeader } from "~/components/layout/PageHeader";
import { useAppStore } from "~/stores/app-store";

export const Route = createFileRoute("/project/$projectSlug/session/new")({
  component: NewChatPage,
});

function NewChatPage() {
  const { projectSlug } = Route.useParams();
  const project = useAppStore((s) => s.projects.find((p) => p.slug === projectSlug));

  if (!project) {
    return (
      <div className="flex flex-col h-full">
        <PageHeader />
        <div className="flex-1 flex items-center justify-center text-muted-foreground">
          <p className="text-sm">Project not found</p>
        </div>
      </div>
    );
  }

  return <NewChatPanel projectId={project.id} projectSlug={projectSlug} />;
}
