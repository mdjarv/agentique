import { Link } from "@tanstack/react-router";
import { Brain, Clock, Cpu, Ellipsis, FolderPlus, Hash } from "lucide-react";
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
  // Memory's home is the assistant's header, and this is the case where that
  // home does not exist — see the row below.
  const brainEnabled = useFeatureStore((s) => s.features.brain);
  const assistantEnabled = useFeatureStore((s) => s.features.assistant);
  const orphanedMemory = brainEnabled && !assistantEnabled;

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
              className="size-7 rounded-md flex items-center justify-center transition-colors hover:bg-muted/50 cursor-pointer text-muted-foreground hover:text-foreground"
            >
              <Ellipsis className="size-4" />
            </button>
          </DropdownMenuTrigger>
          {/*
            The menu lists PLACES WHERE WORK LIVES, and nothing else.

            What a thing *is* decides where it goes. Projects and Templates are
            registrations — a path, a name, a saved prompt, nothing that changes
            on its own — so they are in Settings beside Machines. Storage and
            Settings are the machine's own housekeeping and already sit in the
            footer, behind the account button and the disk meter; a third way in
            was only length. Discussions is an action taken on a set of personas,
            so it is an entry point on the Teams page rather than a peer of it.

            What is left all reports something: channels with traffic, loops
            that run. Memory is normally not here: it lives under the
            assistant, at /assistant/memory, and its flare rides the
            assistant's own row. The assistant is not here because it has a
            home already: the row above the footer, where the operator's
            companion sits (see VoiceDock). One destination, one home.

            The exception is the brain on with the assistant OFF, a
            configuration the server explicitly supports and logs about. The
            assistant's row is not drawn then (VoiceDock), so the header that
            carries the Memory link is unreachable and the page exists only as
            a URL — which on the phone, where this app is an installed PWA with
            no address bar, is a page that does not exist at all. The one-home
            rule is about two homes competing, not about a destination having
            none, so Memory comes back here for exactly that case: still one
            home, chosen by which owner exists.
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
              <Link to="/teams">
                <Hash className="size-3.5" />
                Teams
                <span className="ml-auto text-muted-foreground-faint">channels & personas</span>
              </Link>
            </DropdownMenuItem>
            <DropdownMenuItem asChild className="text-xs gap-2" onSelect={dismissSidebar}>
              <Link to="/schedules">
                <Clock className="size-3.5" />
                Schedules
                <span className="ml-auto text-muted-foreground-faint">loops</span>
              </Link>
            </DropdownMenuItem>
            {orphanedMemory && (
              <DropdownMenuItem asChild className="text-xs gap-2" onSelect={dismissSidebar}>
                <Link to="/assistant/memory">
                  <Brain className="size-3.5" />
                  Memory
                  <span className="ml-auto text-muted-foreground-faint">what it knows</span>
                </Link>
              </DropdownMenuItem>
            )}
          </DropdownMenuContent>
        </DropdownMenu>
        <NewProjectDialog open={newProjectOpen} onOpenChange={setNewProjectOpen} />
      </div>
    </div>
  );
}
