import { createFileRoute } from "@tanstack/react-router";
import { FolderPlus, MousePointerClick } from "lucide-react";
import { PageHeader } from "~/components/layout/PageHeader";
import { Button } from "~/components/ui/button";
import { useIsMobile } from "~/hooks/useIsMobile";
import { useAppStore } from "~/stores/app-store";

export const Route = createFileRoute("/")({
  component: HomePage,
});

function HomePage() {
  const isMobile = useIsMobile();
  const setSidebarOpen = useAppStore((s) => s.setSidebarOpen);
  const projects = useAppStore((s) => s.projects);
  const hasProjects = projects.length > 0;

  return (
    <div className="flex flex-col h-full">
      <PageHeader>
        <span className="font-semibold">Agentique</span>
      </PageHeader>
      <div className="flex-1 flex flex-col items-center justify-center gap-6 px-4">
        <MousePointerClick className="h-10 w-10 text-muted-foreground/20" />
        <div className="text-center space-y-1.5">
          <p className="text-muted-foreground text-sm">
            {hasProjects
              ? "Select a session from the sidebar, or start a new one"
              : "Create a project to get started"}
          </p>
        </div>
        {isMobile && !hasProjects && (
          <Button onClick={() => setSidebarOpen(true)}>
            <FolderPlus className="h-4 w-4 mr-2" />
            New project
          </Button>
        )}
      </div>
    </div>
  );
}
