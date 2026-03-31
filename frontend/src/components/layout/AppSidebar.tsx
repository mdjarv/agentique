import { Link } from "@tanstack/react-router";
import { Cpu } from "lucide-react";
import { NewProjectDialog } from "~/components/layout/NewProjectDialog";
import { ProjectList } from "~/components/layout/ProjectList";
import { SidebarFooter } from "~/components/layout/SidebarFooter";
import { TagFilterBar } from "~/components/layout/TagFilterBar";
import { cn } from "~/lib/utils";

interface AppSidebarProps {
  className?: string;
}

export function AppSidebar({ className }: AppSidebarProps) {
  return (
    <div className={cn("bg-sidebar/80 backdrop-blur-md flex flex-col h-full", className)}>
      <div className="px-4 py-3 border-b flex items-center justify-between">
        <Link to="/" className="flex items-center gap-2.5">
          <Cpu className="size-5 text-primary" />
          <span
            className="text-lg font-semibold tracking-tight bg-gradient-to-r from-primary to-agent bg-clip-text text-transparent"
            style={{ fontFamily: "'Space Grotesk', sans-serif" }}
          >
            Agentique
          </span>
        </Link>
        <NewProjectDialog />
      </div>
      <TagFilterBar />
      <div className="flex-1 overflow-y-auto">
        <ProjectList />
      </div>
      <SidebarFooter />
    </div>
  );
}
