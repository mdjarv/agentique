import { useParams } from "@tanstack/react-router";
import { useCallback, useEffect, useState } from "react";
import { ProjectTreeItem } from "~/components/layout/ProjectTreeItem";
import { useProjects } from "~/hooks/useProjects";

export function ProjectList() {
  const projects = useProjects();
  const params = useParams({ strict: false });
  const activeProjectId = (params as { projectId?: string }).projectId;
  const [expandedIds, setExpandedIds] = useState<Set<string>>(
    () => new Set(projects.map((p) => p.id)),
  );

  // Expand newly added projects automatically.
  useEffect(() => {
    setExpandedIds((prev) => {
      const next = new Set(prev);
      let changed = false;
      for (const p of projects) {
        if (!next.has(p.id)) {
          next.add(p.id);
          changed = true;
        }
      }
      return changed ? next : prev;
    });
  }, [projects]);

  const toggleExpand = useCallback((id: string) => {
    setExpandedIds((prev) => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });
  }, []);

  if (projects.length === 0) {
    return <div className="p-4 text-sm text-sidebar-foreground/70">No projects yet</div>;
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
