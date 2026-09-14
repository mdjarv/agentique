import { createFileRoute, Link } from "@tanstack/react-router";
import { FolderOpen, MessageSquarePlus, Settings } from "lucide-react";
import type { ReactNode } from "react";
import { ProjectGitPanel } from "~/components/chat/git/ProjectGitPanel";
import { PageHeader } from "~/components/layout/PageHeader";
import { Button } from "~/components/ui/button";
import { Tooltip, TooltipContent, TooltipTrigger } from "~/components/ui/tooltip";
import { isFolderProject } from "~/lib/project-kind";
import { useAppStore } from "~/stores/app-store";

export const Route = createFileRoute("/project/$projectSlug/")({
  component: ProjectIndex,
});

const HEADER_ICON =
  "size-7 rounded-md flex items-center justify-center text-muted-foreground hover:text-foreground hover:bg-muted/50 transition-colors shrink-0";

function HeaderLink({ label, children }: { label: string; children: ReactNode }) {
  return (
    <Tooltip>
      <TooltipTrigger asChild>{children}</TooltipTrigger>
      <TooltipContent>{label}</TooltipContent>
    </Tooltip>
  );
}

/**
 * The project's own checkout, outside any session's worktree: its branch, its
 * uncommitted files, its remote. Reached from /projects.
 */
function ProjectIndex() {
  const { projectSlug } = Route.useParams();
  const project = useAppStore((s) => s.projects.find((p) => p.slug === projectSlug));
  const gitStatus = useAppStore((s) => (project ? s.projectGitStatus[project.id] : undefined));

  return (
    <div className="flex flex-col h-full">
      <PageHeader>
        <span className="font-semibold truncate flex-1">{project?.name ?? projectSlug}</span>
        {project && (
          <>
            <HeaderLink label="Browse files">
              <Link
                to="/project/$projectSlug/files"
                params={{ projectSlug: project.slug }}
                aria-label="Browse files"
                className={HEADER_ICON}
              >
                <FolderOpen className="size-4" />
              </Link>
            </HeaderLink>
            <HeaderLink label="Project settings">
              <Link
                to="/project/$projectSlug/settings"
                params={{ projectSlug: project.slug }}
                aria-label="Project settings"
                className={HEADER_ICON}
              >
                <Settings className="size-4" />
              </Link>
            </HeaderLink>
          </>
        )}
      </PageHeader>
      <div className="flex-1 overflow-y-auto px-4 py-4">
        {project && (
          <div className="mb-3 max-w-lg truncate font-mono text-[11px] text-muted-foreground-faint">
            {project.path}
          </div>
        )}
        {project && gitStatus?.branch ? (
          <ProjectGitPanel
            projectId={project.id}
            projectSlug={project.slug}
            gitStatus={gitStatus}
          />
        ) : (
          <div className="flex flex-col items-center justify-center gap-4 text-muted-foreground py-16">
            <p className="text-sm">
              {isFolderProject(project)
                ? "A plain folder — no git state to show."
                : "Select a session or start a new chat"}
            </p>
          </div>
        )}
        <div className="mt-4 max-w-lg">
          <Button asChild size="sm" variant="outline">
            <Link to="/project/$projectSlug/session/new" params={{ projectSlug }}>
              <MessageSquarePlus className="h-4 w-4" />
              New session
            </Link>
          </Button>
        </div>
      </div>
    </div>
  );
}
