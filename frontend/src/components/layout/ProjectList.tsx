import { useParams, useRouterState } from "@tanstack/react-router";
import { useCallback, useEffect, useState } from "react";
import { ProjectTreeItem } from "~/components/layout/ProjectTreeItem";
import { useGlobalSubscriptions } from "~/hooks/useGlobalSubscriptions";
import { useProjectGitPolling } from "~/hooks/useProjectGitPolling";
import { useProjects } from "~/hooks/useProjects";
import { useChatStore } from "~/stores/chat-store";

export function ProjectList() {
  const projects = useProjects();
  const params = useParams({ strict: false }) as {
    projectSlug?: string;
    sessionShortId?: string;
  };
  const activeProjectSlug = params.projectSlug;
  // Resolve short ID → full UUID for active session highlighting
  const activeSessionId = useChatStore((s) => {
    const shortId = params.sessionShortId;
    if (!shortId) return undefined;
    return Object.keys(s.sessions).find((id) => id.startsWith(shortId));
  });
  const isNewChatRoute = useRouterState({
    select: (s) => s.location.pathname.endsWith("/session/new"),
  });
  useGlobalSubscriptions(projects);
  useProjectGitPolling(projects);

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
          isActive={project.slug === activeProjectSlug}
          isExpanded={expandedIds.has(project.id)}
          onToggleExpand={() => toggleExpand(project.id)}
          activeSessionId={activeSessionId}
          isNewChatActive={project.slug === activeProjectSlug && isNewChatRoute}
        />
      ))}
    </div>
  );
}
