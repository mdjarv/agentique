import { useParams } from "@tanstack/react-router";
import { useState } from "react";
import { NewSessionDialog } from "~/components/chat/NewSessionDialog";
import { ProjectTreeItem } from "~/components/layout/ProjectTreeItem";
import { useProjects } from "~/hooks/useProjects";
import { useWebSocket } from "~/hooks/useWebSocket";
import { createSession } from "~/lib/session-actions";

export function ProjectList() {
  const projects = useProjects();
  const params = useParams({ strict: false });
  const activeProjectId = (params as { projectId?: string }).projectId;
  const ws = useWebSocket();

  const [newSessionProjectId, setNewSessionProjectId] = useState<string | null>(null);

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
          onNewSession={() => setNewSessionProjectId(project.id)}
        />
      ))}
      <NewSessionDialog
        open={newSessionProjectId !== null}
        onOpenChange={(open) => {
          if (!open) setNewSessionProjectId(null);
        }}
        onSubmit={async (name, worktree, branch) => {
          if (newSessionProjectId) {
            await createSession(ws, newSessionProjectId, name, worktree, branch);
          }
          setNewSessionProjectId(null);
        }}
      />
    </div>
  );
}
