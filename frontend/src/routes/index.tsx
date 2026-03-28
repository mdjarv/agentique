import { createFileRoute } from "@tanstack/react-router";
import { FolderPlus } from "lucide-react";
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
      <div className="flex-1 flex flex-col items-center justify-center gap-4 px-4">
        <p className="text-muted-foreground text-lg text-center">
          {hasProjects ? "Select a project to get started" : "Create a project to get started"}
        </p>
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
