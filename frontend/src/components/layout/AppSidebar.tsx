import { ConnectionIndicator } from "~/components/layout/ConnectionIndicator";
import { NewProjectDialog } from "~/components/layout/NewProjectDialog";
import { ProjectList } from "~/components/layout/ProjectList";
import { TooltipProvider } from "~/components/ui/tooltip";

export function AppSidebar() {
  return (
    <TooltipProvider>
      <div className="w-72 border-r bg-sidebar flex flex-col h-full">
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
    </TooltipProvider>
  );
}
