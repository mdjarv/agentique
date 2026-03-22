import { useParams } from "@tanstack/react-router";
import { ProjectTreeItem } from "~/components/layout/ProjectTreeItem";
import { useProjects } from "~/hooks/useProjects";
import { useChatStore } from "~/stores/chat-store";

export function ProjectList() {
  const projects = useProjects();
  const params = useParams({ strict: false });
  const activeProjectId = (params as { projectId?: string }).projectId;

  if (projects.length === 0) {
    return <div className="p-4 text-sm text-muted-foreground">No projects yet</div>;
  }

  return (
    <div className="p-2 space-y-1">
      {projects.map((project) => (
        <ProjectTreeItem
          key={project.id}
          project={project}
          isActive={project.id === activeProjectId}
          onNewSession={() => useChatStore.getState().createDraft(project.id)}
        />
      ))}
    </div>
  );
}
