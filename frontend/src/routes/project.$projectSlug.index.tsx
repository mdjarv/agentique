import { createFileRoute, useNavigate } from "@tanstack/react-router";
import { MessageSquarePlus, PanelLeft } from "lucide-react";
import { Button } from "~/components/ui/button";
import { useIsMobile } from "~/hooks/useIsMobile";
import { useAppStore } from "~/stores/app-store";

export const Route = createFileRoute("/project/$projectSlug/")({
  component: ProjectIndex,
});

function ProjectIndex() {
  const { projectSlug } = Route.useParams();
  const navigate = useNavigate();
  const isMobile = useIsMobile();
  const projectsLoaded = useAppStore((s) => s.projectsLoaded);
  const projectExists = useAppStore((s) => s.projects.some((p) => p.slug === projectSlug));
  const setSidebarOpen = useAppStore((s) => s.setSidebarOpen);

  if (projectsLoaded && !projectExists) {
    return (
      <div className="flex flex-col h-full items-center justify-center text-muted-foreground">
        <p className="text-sm">Project not found</p>
      </div>
    );
  }

  return (
    <div className="flex flex-col h-full items-center justify-center gap-4 text-muted-foreground px-4">
      <p className="text-sm">Select a session or start a new chat</p>
      {isMobile && (
        <div className="flex gap-2">
          <Button variant="outline" onClick={() => setSidebarOpen(true)}>
            <PanelLeft className="h-4 w-4 mr-2" />
            Sessions
          </Button>
          <Button
            onClick={() =>
              navigate({
                to: "/project/$projectSlug/session/new",
                params: { projectSlug },
              })
            }
          >
            <MessageSquarePlus className="h-4 w-4 mr-2" />
            New chat
          </Button>
        </div>
      )}
    </div>
  );
}
