import { Link } from "@tanstack/react-router";
import { Cpu, FileText, Hash, LayoutList } from "lucide-react";

import { NewProjectDialog } from "~/components/layout/project/NewProjectDialog";
import { SidebarFooter } from "~/components/layout/SidebarFooter";
import { Tooltip, TooltipContent, TooltipTrigger } from "~/components/ui/tooltip";
import { cn } from "~/lib/utils";
import { useChannelStore } from "~/stores/channel-store";
import { type SidebarTab, useUIStore } from "~/stores/ui-store";
import { FolderSidebar } from "./variants/FolderSidebar";
import { TeamsTab } from "./variants/folder-sidebar/TeamsTab";

interface AppSidebarProps {
  className?: string;
}

export function AppSidebar({ className }: AppSidebarProps) {
  const sidebarTab = useUIStore((s) => s.sidebarTab);

  return (
    <div className={cn("bg-sidebar/80 backdrop-blur-md flex h-full flex-col", className)}>
      <SidebarHeader />
      <SidebarTabBar active={sidebarTab} />
      {sidebarTab === "teams" ? <TeamsTab /> : <FolderSidebar />}
      <SidebarFooter />
    </div>
  );
}

function SidebarHeader() {
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
      </div>
    </div>
  );
}

const TAB_ITEMS: { id: SidebarTab; label: string; icon: typeof LayoutList }[] = [
  { id: "sessions", label: "Sessions", icon: LayoutList },
  { id: "teams", label: "Teams", icon: Hash },
];

function SidebarTabBar({ active }: { active: SidebarTab }) {
  const channelCount = useChannelStore((s) => Object.keys(s.channels).length);

  return (
    <div className="flex items-center border-b px-2 gap-0.5 h-8 shrink-0">
      {TAB_ITEMS.map(({ id, label, icon: Icon }) => (
        <button
          key={id}
          type="button"
          onClick={() => useUIStore.getState().setSidebarTab(id)}
          className={cn(
            "flex items-center gap-1.5 px-2.5 py-1 text-xs rounded-md transition-colors cursor-pointer",
            active === id
              ? "bg-primary/10 text-primary font-medium"
              : "text-muted-foreground hover:text-foreground hover:bg-muted/30",
          )}
        >
          <Icon className="size-3.5" />
          {label}
          {id === "teams" && channelCount > 0 && (
            <span className="text-[10px] tabular-nums text-primary/60 ml-0.5">{channelCount}</span>
          )}
        </button>
      ))}
    </div>
  );
}
