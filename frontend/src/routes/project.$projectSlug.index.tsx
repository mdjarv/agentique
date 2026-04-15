import { createFileRoute, Link, useNavigate } from "@tanstack/react-router";
import { MessageSquarePlus, Settings } from "lucide-react";
import { ProjectGitPanel } from "~/components/chat/git/ProjectGitPanel";
import { PageHeader } from "~/components/layout/PageHeader";
import { Button } from "~/components/ui/button";
import { Tooltip, TooltipContent, TooltipTrigger } from "~/components/ui/tooltip";
import { useIsMobile } from "~/hooks/useIsMobile";
import { useAppStore } from "~/stores/app-store";

export const Route = createFileRoute("/project/$projectSlug/")({
  component: ProjectIndex,
});

function ProjectIndex() {
  const { projectSlug } = Route.useParams();
  const navigate = useNavigate();
  const isMobile = useIsMobile();
  const project = useAppStore((s) => s.projects.find((p) => p.slug === projectSlug));
  const gitStatus = useAppStore((s) => (project ? s.projectGitStatus[project.id] : undefined));

  return (
    <div className="flex flex-col h-full">
      <PageHeader>
        <span className="font-semibold truncate flex-1">{project?.name ?? projectSlug}</span>
        {project && (
          <Tooltip>
            <TooltipTrigger asChild>
              <Link
                to="/project/$projectSlug/settings"
                params={{ projectSlug: project.slug }}
                className="size-7 rounded-md flex items-center justify-center text-muted-foreground hover:text-foreground hover:bg-muted/50 transition-colors shrink-0"
              >
                <Settings className="size-4" />
              </Link>
            </TooltipTrigger>
            <TooltipContent>Project settings</TooltipContent>
          </Tooltip>
        )}
      </PageHeader>
      <div className="flex-1 overflow-y-auto px-4 py-4">
        {project && gitStatus?.branch ? (
          <ProjectGitPanel projectId={project.id} gitStatus={gitStatus} />
        ) : (
          <div className="flex flex-col items-center justify-center gap-4 text-muted-foreground h-full">
            <p className="text-sm">Select a session or start a new chat</p>
            {isMobile && (
              <Button
                onClick={() =>
                  navigate({
                    to: "/project/$projectSlug/session/new",
                    params: { projectSlug },
                  })
                }
              >
                <MessageSquarePlus className="h-4 w-4 mr-2" />
                New chat
              </Button>
            )}
          </div>
        )}
      </div>
    </div>
  );
}
