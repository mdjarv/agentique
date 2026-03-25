import { createFileRoute } from "@tanstack/react-router";
import { useAppStore } from "~/stores/app-store";

export const Route = createFileRoute("/project/$projectSlug/")({
  component: ProjectIndex,
});

function ProjectIndex() {
  const { projectSlug } = Route.useParams();
  const projectsLoaded = useAppStore((s) => s.projectsLoaded);
  const projectExists = useAppStore((s) => s.projects.some((p) => p.slug === projectSlug));

  if (projectsLoaded && !projectExists) {
    return (
      <div className="flex flex-col h-full items-center justify-center text-muted-foreground">
        <p className="text-sm">Project not found</p>
      </div>
    );
  }

  return (
    <div className="flex flex-col h-full items-center justify-center text-muted-foreground">
      <p className="text-sm">Select a session or start a new chat</p>
    </div>
  );
}
