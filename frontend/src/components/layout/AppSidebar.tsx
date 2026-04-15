import { Link } from "@tanstack/react-router";
import { Cpu, FileText, Users } from "lucide-react";

import { NewProjectDialog } from "~/components/layout/project/NewProjectDialog";
import { SidebarFooter } from "~/components/layout/SidebarFooter";
import { TeamPanel } from "~/components/team/TeamPanel";
import { Tooltip, TooltipContent, TooltipTrigger } from "~/components/ui/tooltip";
import { cn } from "~/lib/utils";
import { useTeamStore } from "~/stores/team-store";
import { useUIStore } from "~/stores/ui-store";
import { FolderSidebar } from "./variants/FolderSidebar";

interface AppSidebarProps {
  className?: string;
}

export function AppSidebar({ className }: AppSidebarProps) {
  const teamPanelOpen = useUIStore((s) => s.teamPanelOpen);
  const teamsLoaded = useTeamStore((s) => s.loaded);

  return (
    <div className={cn("bg-sidebar/80 backdrop-blur-md flex h-full flex-col", className)}>
      <SidebarHeader teamPanelOpen={teamPanelOpen} teamsLoaded={teamsLoaded} />
      {teamPanelOpen ? <TeamPanel /> : <FolderSidebar />}
      <SidebarFooter />
    </div>
  );
}

function SidebarHeader({
  teamPanelOpen,
  teamsLoaded,
}: {
  teamPanelOpen: boolean;
  teamsLoaded: boolean;
}) {
  return (
    <div className="px-4 border-b flex items-center justify-between h-12">
      <Link to="/" className="flex items-center gap-2.5">
        <Cpu className="size-5 text-primary" />
        <span
          className="text-lg font-semibold tracking-tight bg-gradient-to-r from-primary to-agent bg-clip-text text-transparent"
          style={{ fontFamily: "'Space Grotesk', sans-serif" }}
        >
          Agentique
        </span>
      </Link>
      <div className="flex items-center gap-1">
        <NewProjectDialog />
        <Tooltip>
          <TooltipTrigger asChild>
            <Link
              to="/templates"
              className="size-7 rounded-md flex items-center justify-center transition-colors text-muted-foreground hover:text-foreground hover:bg-muted/50"
            >
              <FileText className="size-4" />
            </Link>
          </TooltipTrigger>
          <TooltipContent side="bottom">Prompt templates</TooltipContent>
        </Tooltip>
        {teamsLoaded && (
          <Tooltip>
            <TooltipTrigger asChild>
              <button
                type="button"
                onClick={() => useUIStore.getState().setTeamPanelOpen(!teamPanelOpen)}
                className={cn(
                  "size-7 rounded-md flex items-center justify-center transition-colors",
                  teamPanelOpen
                    ? "bg-primary/15 text-primary"
                    : "text-muted-foreground hover:text-foreground hover:bg-muted/50",
                )}
              >
                <Users className="size-4" />
              </button>
            </TooltipTrigger>
            <TooltipContent side="bottom">
              {teamPanelOpen ? "Show sessions" : "Show teams"}
            </TooltipContent>
          </Tooltip>
        )}
      </div>
    </div>
  );
}
