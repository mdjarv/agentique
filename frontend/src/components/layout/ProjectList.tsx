import { useParams, useRouterState } from "@tanstack/react-router";
import { useCallback, useMemo } from "react";
import type { Project } from "~/lib/types";
import { useAppStore } from "~/stores/app-store";
import { useChatStore } from "~/stores/chat-store";
import { useUIStore } from "~/stores/ui-store";
import { ProjectTreeItem } from "./ProjectTreeItem";

function useFilteredSortedProjects(): { favorites: Project[]; rest: Project[] } {
  const projects = useAppStore((s) => s.projects);
  const projectTags = useAppStore((s) => s.projectTags);
  const activeTagFilters = useUIStore((s) => s.activeTagFilters);

  const tags = useAppStore((s) => s.tags);

  return useMemo(() => {
    let filtered = projects;

    // Filter by tags (OR logic: show projects matching ANY selected tag).
    // Ignore stale filter IDs that reference tags no longer in the store.
    if (activeTagFilters.length > 0) {
      const knownTagIds = new Set(tags.map((t) => t.id));
      const validFilters = activeTagFilters.filter((id) => knownTagIds.has(id));
      if (validFilters.length > 0) {
        const filterSet = new Set(validFilters);
        const projectsWithMatchingTag = new Set(
          projectTags.filter((pt) => filterSet.has(pt.tag_id)).map((pt) => pt.project_id),
        );
        filtered = filtered.filter((p) => projectsWithMatchingTag.has(p.id));
      }
    }

    // Sort: favorites first, then alphabetical by name
    const sorted = [...filtered].sort((a, b) => {
      if (a.favorite !== b.favorite) return b.favorite - a.favorite;
      return a.name.localeCompare(b.name);
    });

    const favorites = sorted.filter((p) => p.favorite === 1);
    const rest = sorted.filter((p) => p.favorite !== 1);
    return { favorites, rest };
  }, [projects, projectTags, activeTagFilters, tags]);
}

export function ProjectList() {
  const { favorites, rest } = useFilteredSortedProjects();
  const allVisible = useMemo(() => [...favorites, ...rest], [favorites, rest]);

  const params = useParams({ strict: false }) as {
    projectSlug?: string;
    sessionShortId?: string;
  };
  const activeProjectSlug = params.projectSlug;
  const activeSessionId = useChatStore((s) => {
    const shortId = params.sessionShortId;
    if (!shortId) return undefined;
    return Object.keys(s.sessions).find((id) => id.startsWith(shortId));
  });
  const isNewChatRoute = useRouterState({
    select: (s) => s.location.pathname.endsWith("/session/new"),
  });

  const collapsedProjectIds = useUIStore((s) => s.collapsedProjectIds);
  const expandedIds = useMemo(() => {
    const collapsed = new Set(collapsedProjectIds);
    return new Set(allVisible.filter((p) => !collapsed.has(p.id)).map((p) => p.id));
  }, [collapsedProjectIds, allVisible]);

  const toggleExpand = useCallback(
    (id: string) => {
      useUIStore.getState().setProjectCollapsed(id, expandedIds.has(id));
    },
    [expandedIds],
  );

  const projects = useAppStore((s) => s.projects);
  if (projects.length === 0) {
    return <div className="p-4 text-sm text-sidebar-foreground/70">No projects yet</div>;
  }

  if (allVisible.length === 0) {
    return <div className="p-4 text-sm text-sidebar-foreground/70">No matching projects</div>;
  }

  return (
    <div className="py-1">
      {favorites.map((project) => (
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
      {favorites.length > 0 && rest.length > 0 && (
        <div className="mx-3 my-1 border-t border-sidebar-border/30" />
      )}
      {rest.map((project) => (
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
