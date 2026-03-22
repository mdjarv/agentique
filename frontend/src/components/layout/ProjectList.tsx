import { useNavigate, useParams } from "@tanstack/react-router";
import { FolderOpen, Trash2 } from "lucide-react";
import { useProjects } from "~/hooks/useProjects";
import { deleteProject } from "~/lib/api";
import { useAppStore } from "~/stores/app-store";

export function ProjectList() {
  const projects = useProjects();
  const navigate = useNavigate();
  const removeProject = useAppStore((s) => s.removeProject);
  const params = useParams({ strict: false });
  const activeProjectId = (params as { projectId?: string }).projectId;

  const handleDelete = async (e: React.MouseEvent, id: string) => {
    e.stopPropagation();
    try {
      await deleteProject(id);
      removeProject(id);
      if (activeProjectId === id) {
        navigate({ to: "/" });
      }
    } catch (err) {
      console.error("Failed to delete project:", err);
    }
  };

  const handleProjectClick = (projectId: string) => {
    navigate({ to: "/project/$projectId", params: { projectId } });
  };

  const handleProjectKeyDown = (e: React.KeyboardEvent, projectId: string) => {
    if (e.key === "Enter" || e.key === " ") {
      e.preventDefault();
      handleProjectClick(projectId);
    }
  };

  if (projects.length === 0) {
    return <div className="p-4 text-sm text-muted-foreground">No projects yet</div>;
  }

  return (
    <div className="p-2 space-y-1">
      {projects.map((project) => {
        return (
          <div
            key={project.id}
            // biome-ignore lint/a11y/useSemanticElements: div with role=button avoids invalid nested button HTML
            role="button"
            tabIndex={0}
            onClick={() => handleProjectClick(project.id)}
            onKeyDown={(e) => handleProjectKeyDown(e, project.id)}
            className={`w-full text-left rounded-md px-3 py-2 group hover:bg-accent transition-colors cursor-pointer ${
              activeProjectId === project.id ? "bg-accent" : ""
            }`}
          >
            <div className="flex items-center justify-between">
              <div className="flex items-center gap-2 min-w-0">
                <FolderOpen className="h-4 w-4 shrink-0" />
                <span className="text-sm font-medium truncate">{project.name}</span>
              </div>
              <button
                type="button"
                aria-label="Delete project"
                onClick={(e) => handleDelete(e, project.id)}
                className="opacity-0 group-hover:opacity-100 p-1 rounded hover:bg-destructive hover:text-destructive-foreground transition-opacity"
              >
                <Trash2 className="h-3.5 w-3.5" />
              </button>
            </div>
            <p className="text-xs text-muted-foreground truncate mt-0.5 pl-6">{project.path}</p>
          </div>
        );
      })}
    </div>
  );
}
