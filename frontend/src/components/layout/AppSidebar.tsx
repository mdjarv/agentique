import { Link } from "@tanstack/react-router";
import {
  Brain,
  Clock,
  Cpu,
  Ellipsis,
  FileText,
  FolderGit2,
  FolderPlus,
  HardDrive,
  Hash,
  MessagesSquare,
  Settings as SettingsIcon,
} from "lucide-react";
import { useEffect, useRef, useState } from "react";

import { SyncDock } from "~/components/layout/git/SyncDock";
import { NewProjectDialog } from "~/components/layout/project/NewProjectDialog";
import { SidebarFooter } from "~/components/layout/SidebarFooter";
import { NewSessionButton, ThreadSidebar } from "~/components/layout/thread-sidebar";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "~/components/ui/dropdown-menu";
import { VoiceDock } from "~/components/voice/VoiceDock";
import { cn } from "~/lib/utils";
import { useBrainStore } from "~/stores/brain-store";

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
  return (
    <div className={cn("bg-sidebar/80 backdrop-blur-md flex h-full flex-col", className)}>
      <SidebarHeader />
      <ThreadSidebar />
      {/* Sessions, then settled work, then repos, then system: the dock's
          growth pushes down into the footer, never into the session list. */}
      <SyncDock />
      {/* The call sits below settled work and above the system line: it is
          always-true state, but it is the operator's, not the machine's. */}
      <VoiceDock />
      <SidebarFooter />
    </div>
  );
}

function SidebarHeader() {
  const flaring = useBrainFlare();
  const [newProjectOpen, setNewProjectOpen] = useState(false);

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
      <div className="flex items-center gap-1.5">
        <NewSessionButton />
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
            <DropdownMenuItem className="text-xs gap-2" onSelect={() => setNewProjectOpen(true)}>
              <FolderPlus className="size-3.5" />
              New project
              <span className="ml-auto text-muted-foreground-faint">add a repo</span>
            </DropdownMenuItem>
            <DropdownMenuSeparator />
            <DropdownMenuItem asChild className="text-xs gap-2">
              <Link to="/projects">
                <FolderGit2 className="size-3.5" />
                Projects
                <span className="ml-auto text-muted-foreground-faint">all repos</span>
              </Link>
            </DropdownMenuItem>
            <DropdownMenuItem asChild className="text-xs gap-2">
              <Link to="/teams">
                <Hash className="size-3.5" />
                Teams
                <span className="ml-auto text-muted-foreground-faint">channels</span>
              </Link>
            </DropdownMenuItem>
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
            <DropdownMenuItem asChild className="text-xs gap-2">
              <Link to="/settings">
                <SettingsIcon className="size-3.5" />
                Settings
                <span className="ml-auto text-muted-foreground-faint">machines & account</span>
              </Link>
            </DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
        <NewProjectDialog open={newProjectOpen} onOpenChange={setNewProjectOpen} />
      </div>
    </div>
  );
}
