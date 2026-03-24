import { useParams } from "@tanstack/react-router";
import { useCallback, useEffect, useState } from "react";
import { ProjectTreeItem } from "~/components/layout/ProjectTreeItem";
import { useProjects } from "~/hooks/useProjects";

export function ProjectList() {
  const projects = useProjects();
  const params = useParams({ strict: false });
  const activeProjectId = (params as { projectId?: string }).projectId;
  const [expandedIds, setExpandedIds] = useState<Set<string>>(new Set());

  // Auto-expand the active project when it changes.
  useEffect(() => {
    if (activeProjectId) {
      setExpandedIds((prev) => {
        if (prev.has(activeProjectId)) return prev;
        return new Set(prev).add(activeProjectId);
      });
    }
  }, [activeProjectId]);

  const toggleExpand = useCallback((id: string) => {
    setExpandedIds((prev) => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });
  }, []);

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
          isExpanded={expandedIds.has(project.id)}
          onToggleExpand={() => toggleExpand(project.id)}
        />
      ))}
    </div>
  );
}
