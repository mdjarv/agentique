import { createFileRoute, useNavigate } from "@tanstack/react-router";
import { MessageSquarePlus } from "lucide-react";
import { PageHeader } from "~/components/layout/PageHeader";
import { Button } from "~/components/ui/button";
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

  return (
    <div className="flex flex-col h-full">
      <PageHeader>
        <span className="font-semibold truncate">{project?.name ?? projectSlug}</span>
      </PageHeader>
      <div className="flex-1 flex flex-col items-center justify-center gap-4 text-muted-foreground px-4">
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
    </div>
  );
}
