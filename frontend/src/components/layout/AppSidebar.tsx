import { Link } from "@tanstack/react-router";
import { Brain, Clock, Cpu, Ellipsis, FolderGit2, FolderPlus, Hash } from "lucide-react";
import { useState } from "react";

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
import { useMemoryFlare } from "~/hooks/useMemoryFlare";
import { dismissSidebar } from "~/lib/sidebar-nav";
import { cn } from "~/lib/utils";
import { useFeatureStore } from "~/stores/feature-store";

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
      {/* The operator's companion sits below settled work and above the
          system line: always-true state, but the operator's, not the
          machine's. It is the assistant's row, and a call is that row awake. */}
      <VoiceDock />
      <SidebarFooter />
    </div>
  );
}

function SidebarHeader() {
  const [newProjectOpen, setNewProjectOpen] = useState(false);
  // The brain is off by default, and off means the server mounts no
  // /api/brain routes — so the row is absent rather than leading somewhere
  // that answers the SPA.
  const brainEnabled = useFeatureStore((s) => s.features.brain);
  const flaring = useMemoryFlare();

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
                // The memory flare survives the collapse: it lights the trigger
                // because the menu is the only thing visible.
                flaring
                  ? "text-primary brain-flare"
                  : "text-muted-foreground hover:text-foreground",
              )}
            >
              <Ellipsis className="size-4" />
            </button>
          </DropdownMenuTrigger>
          {/*
            The menu lists PLACES WHERE WORK LIVES, and nothing else.

            What a thing *is* decides where it goes. A project's registration
            and Templates are registrations — a path, a name, a saved prompt,
            nothing that changes on its own — so they are in Settings beside
            Machines. The project's *checkout* is not: branches move and files
            go uncommitted on their own, so /projects (every checkout's state)
            is listed here. Storage and
            Settings are the machine's own housekeeping and already sit in the
            footer, behind the account button and the disk meter; a third way in
            was only length. Discussions is an action taken on a set of personas,
            so it is an entry point on the Teams page rather than a peer of it.

            What is left all reports something: channels with traffic, a brain
            that flares, loops that run. The assistant is not here because it
            has a home already: the row above the footer, where the operator's
            companion sits (see VoiceDock).

            Memory is the one destination with two ways in, by the operator's
            choice (2026-09-15): this row, because the brain reports on its own
            — consolidation runs on a timer and the assistant remembers
            mid-conversation, and the flare on this menu's trigger is that
            report — and a button in the assistant thread's header, because it
            is the assistant's memory and the thread is where a recalled fact
            is read. This row is also the one that exists with the assistant
            OFF, where the thread's header does not.
          */}
          {/* `useSidebarDismissOnNavigate` closes the mobile sheet on arrival;
              these dismiss on the click as well, because a menu item can name
              the page you are already on, and that navigation never happens. */}
          <DropdownMenuContent align="end" className="w-56">
            <DropdownMenuItem className="text-xs gap-2" onSelect={() => setNewProjectOpen(true)}>
              <FolderPlus className="size-3.5" />
              New project
              <span className="ml-auto text-muted-foreground-faint">add a repo</span>
            </DropdownMenuItem>
            <DropdownMenuSeparator />
            <DropdownMenuItem asChild className="text-xs gap-2" onSelect={dismissSidebar}>
              <Link to="/projects">
                <FolderGit2 className="size-3.5" />
                Projects
                <span className="ml-auto text-muted-foreground-faint">checkouts</span>
              </Link>
            </DropdownMenuItem>
            <DropdownMenuItem asChild className="text-xs gap-2" onSelect={dismissSidebar}>
              <Link to="/teams">
                <Hash className="size-3.5" />
                Teams
                <span className="ml-auto text-muted-foreground-faint">channels & personas</span>
              </Link>
            </DropdownMenuItem>
            {brainEnabled && (
              <DropdownMenuItem asChild className="text-xs gap-2" onSelect={dismissSidebar}>
                <Link to="/assistant/memory">
                  <Brain className={cn("size-3.5", flaring && "text-primary brain-flare")} />
                  Memory
                  <span className="ml-auto text-muted-foreground-faint">what it knows</span>
                </Link>
              </DropdownMenuItem>
            )}
            <DropdownMenuItem asChild className="text-xs gap-2" onSelect={dismissSidebar}>
              <Link to="/schedules">
                <Clock className="size-3.5" />
                Schedules
                <span className="ml-auto text-muted-foreground-faint">loops</span>
              </Link>
            </DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
        <NewProjectDialog open={newProjectOpen} onOpenChange={setNewProjectOpen} />
      </div>
    </div>
  );
}
