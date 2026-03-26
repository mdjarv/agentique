import { Cpu } from "lucide-react";
import { NewProjectDialog } from "~/components/layout/NewProjectDialog";
import { ProjectList } from "~/components/layout/ProjectList";
import { SidebarFooter } from "~/components/layout/SidebarFooter";
import { cn } from "~/lib/utils";

interface AppSidebarProps {
  className?: string;
}

export function AppSidebar({ className }: AppSidebarProps) {
  return (
    <div className={cn("bg-sidebar flex flex-col h-full", className)}>
      <div className="px-4 py-3 border-b flex items-center justify-between">
        <div className="flex items-center gap-2.5">
          <Cpu className="size-5 text-[#7aa2f7]" />
          <span
            className="text-lg font-semibold tracking-tight bg-gradient-to-r from-[#7aa2f7] to-[#bb9af7] bg-clip-text text-transparent"
            style={{ fontFamily: "'Space Grotesk', sans-serif" }}
          >
            Agentique
          </span>
        </div>
        <NewProjectDialog />
      </div>
      <div className="flex-1 overflow-y-auto">
        <ProjectList />
      </div>
      <SidebarFooter />
    </div>
  );
}
