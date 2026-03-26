import { ConnectionIndicator } from "~/components/layout/ConnectionIndicator";
import { NewProjectDialog } from "~/components/layout/NewProjectDialog";
import { ProjectList } from "~/components/layout/ProjectList";
import { cn } from "~/lib/utils";

interface AppSidebarProps {
  className?: string;
}

export function AppSidebar({ className }: AppSidebarProps) {
  return (
    <div className={cn("bg-sidebar flex flex-col h-full", className)}>
      <div className="p-4 font-semibold text-lg border-b flex items-center justify-between">
        Agentique
        <NewProjectDialog />
      </div>
      <div className="flex-1 overflow-y-auto">
        <ProjectList />
      </div>
      <div className="p-3 border-t">
        <ConnectionIndicator />
      </div>
    </div>
  );
}
