import { Link, useLocation } from "@tanstack/react-router";
import {
  Brain,
  Clock,
  Cpu,
  Ellipsis,
  FileText,
  HardDrive,
  Hash,
  LayoutList,
  MessagesSquare,
} from "lucide-react";
import { useEffect, useRef, useState } from "react";

import { NewProjectDialog } from "~/components/layout/project/NewProjectDialog";
import { SidebarFooter } from "~/components/layout/SidebarFooter";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "~/components/ui/dropdown-menu";
import { cn } from "~/lib/utils";
import { useBrainStore } from "~/stores/brain-store";
import { useChannelStore } from "~/stores/channel-store";
import { FolderSidebar } from "./variants/FolderSidebar";
import { TeamsTab } from "./variants/folder-sidebar/TeamsTab";

// useBrainFlare returns true for a short window after the brain changes (a memory
// added/edited/removed, or a consolidation applied — anywhere, any tab), so the
// nav button can pulse to signal the brain is alive.
function useBrainFlare(): boolean {
  const flareSeq = useBrainStore((s) => s.flareSeq);
  const [flaring, setFlaring] = useState(false);
  const seenRef = useRef(flareSeq);
  useEffect(() => {
    if (flareSeq === seenRef.current) return; // skip the initial value
    seenRef.current = flareSeq;
    setFlaring(true);
    const t = setTimeout(() => setFlaring(false), 2200);
    return () => clearTimeout(t);
  }, [flareSeq]);
  return flaring;
}

interface AppSidebarProps {
  className?: string;
}

export function AppSidebar({ className }: AppSidebarProps) {
  const pathname = useLocation({ select: (l) => l.pathname });
  const isTeams = pathname.startsWith("/teams");

  return (
    <div className={cn("bg-sidebar/80 backdrop-blur-md flex h-full flex-col", className)}>
      <SidebarHeader />
      <SidebarTabBar isTeams={isTeams} />
      {isTeams ? <TeamsTab /> : <FolderSidebar />}
      <SidebarFooter />
    </div>
  );
}

function SidebarHeader() {
  const flaring = useBrainFlare();
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
        <DropdownMenu>
          <DropdownMenuTrigger asChild>
            <button
              type="button"
              aria-label="More tools"
              className={cn(
                "size-7 rounded-md flex items-center justify-center transition-colors hover:bg-muted/50 cursor-pointer",
                // The brain-alive pulse survives the collapse: it flares the
                // trigger when the menu is the only thing visible.
                flaring
                  ? "text-primary brain-flare"
                  : "text-muted-foreground hover:text-foreground",
              )}
            >
              <Ellipsis className="size-4" />
            </button>
          </DropdownMenuTrigger>
          <DropdownMenuContent align="end" className="w-56">
            <DropdownMenuItem asChild className="text-xs gap-2">
              <Link to="/brain">
                <Brain className={cn("size-3.5", flaring && "text-primary brain-flare")} />
                Brain
                <span className="ml-auto text-muted-foreground-faint">persistent memory</span>
              </Link>
            </DropdownMenuItem>
            <DropdownMenuItem asChild className="text-xs gap-2">
              <Link to="/templates">
                <FileText className="size-3.5" />
                Templates
                <span className="ml-auto text-muted-foreground-faint">prompts</span>
              </Link>
            </DropdownMenuItem>
            <DropdownMenuItem asChild className="text-xs gap-2">
              <Link to="/discussions">
                <MessagesSquare className="size-3.5" />
                Discussions
                <span className="ml-auto text-muted-foreground-faint">groups</span>
              </Link>
            </DropdownMenuItem>
            <DropdownMenuItem asChild className="text-xs gap-2">
              <Link to="/schedules">
                <Clock className="size-3.5" />
                Schedules
                <span className="ml-auto text-muted-foreground-faint">loops</span>
              </Link>
            </DropdownMenuItem>
            <DropdownMenuSeparator />
            <DropdownMenuItem asChild className="text-xs gap-2">
              <Link to="/storage">
                <HardDrive className="size-3.5" />
                Storage
                <span className="ml-auto text-muted-foreground-faint">disk & worktrees</span>
              </Link>
            </DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
      </div>
    </div>
  );
}

function SidebarTabBar({ isTeams }: { isTeams: boolean }) {
  const channelCount = useChannelStore((s) => Object.keys(s.channels).length);

  const baseClass =
    "flex items-center gap-1.5 px-2.5 py-1 text-xs rounded-md transition-colors cursor-pointer";
  const activeClass = "bg-primary/10 text-primary font-medium";
  const inactiveClass = "text-muted-foreground hover:text-foreground hover:bg-muted/30";

  return (
    <div className="flex items-center border-b px-2 gap-0.5 h-8 shrink-0">
      <Link to="/" className={cn(baseClass, !isTeams ? activeClass : inactiveClass)}>
        <LayoutList className="size-3.5" />
        Sessions
      </Link>
      <Link to="/teams" className={cn(baseClass, isTeams ? activeClass : inactiveClass)}>
        <Hash className="size-3.5" />
        Teams
        {channelCount > 0 && (
          <span className="text-[10px] tabular-nums text-primary/60 ml-0.5">{channelCount}</span>
        )}
      </Link>
    </div>
  );
}
