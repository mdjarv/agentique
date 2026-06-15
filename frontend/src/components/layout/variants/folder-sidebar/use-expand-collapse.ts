/**
 * Expand/collapse state for folders and projects, plus the "sticky expand"
 * behavior: once a project gains an active session it is auto-expanded so the
 * user's inline session view doesn't silently collapse when activity ends.
 *
 * The auto-expand is recorded once per project via a ref — without it the effect
 * re-evaluates every project on every sidebar recompute (frequent during active
 * turns). Combined with `setManyProjectsExpanded`'s no-op guard (it returns the
 * same state object when nothing changes), this keeps the effect from thrashing.
 */
import { useCallback, useEffect, useMemo, useRef } from "react";
import { useUIStore } from "~/stores/ui-store";
import type { FolderGroup, ProjectEntry } from "./types";

interface UseExpandCollapseArgs {
  orderedFolders: FolderGroup[];
  ungrouped: ProjectEntry[];
}

export interface ExpandCollapse {
  /** Whether a folder is expanded (defaults to true for unseen folders). */
  isFolderExpanded: (name: string) => boolean;
  /** Whether a project is expanded; falls back to `hasActive` when untouched. */
  isProjectExpanded: (projectId: string, hasActive: boolean) => boolean;
  toggleFolder: (name: string) => void;
  toggleProject: (id: string) => void;
  expandProject: (id: string) => void;
  /** True when any folder or project is currently expanded. */
  anythingExpanded: boolean;
  toggleCollapseAll: () => void;
}

export function useExpandCollapse({
  orderedFolders,
  ungrouped,
}: UseExpandCollapseArgs): ExpandCollapse {
  const expandedFolders = useUIStore((s) => s.expandedFolders);
  const expandedProjects = useUIStore((s) => s.expandedProjects);
  const setFolderExpanded = useUIStore((s) => s.setFolderExpanded);
  const setProjectExpanded = useUIStore((s) => s.setProjectExpanded);
  const setManyFoldersExpanded = useUIStore((s) => s.setManyFoldersExpanded);
  const setManyProjectsExpanded = useUIStore((s) => s.setManyProjectsExpanded);

  const isFolderExpanded = useCallback(
    (name: string) => expandedFolders[name] ?? true,
    [expandedFolders],
  );

  const isProjectExpanded = useCallback(
    (projectId: string, hasActive: boolean) => {
      if (projectId in expandedProjects) return expandedProjects[projectId] ?? false;
      return hasActive;
    },
    [expandedProjects],
  );

  const toggleFolder = useCallback(
    (name: string) => setFolderExpanded(name, !(expandedFolders[name] ?? true)),
    [expandedFolders, setFolderExpanded],
  );
  const toggleProject = useCallback(
    (id: string) => setProjectExpanded(id, !expandedProjects[id]),
    [expandedProjects, setProjectExpanded],
  );
  const expandProject = useCallback(
    (id: string) => setProjectExpanded(id, true),
    [setProjectExpanded],
  );

  const allProjectIds = useMemo(() => {
    const ids: string[] = [];
    for (const f of orderedFolders) for (const e of f.projects) ids.push(e.project.id);
    for (const e of ungrouped) ids.push(e.project.id);
    return ids;
  }, [orderedFolders, ungrouped]);

  const allFolderNames = useMemo(() => orderedFolders.map((f) => f.name), [orderedFolders]);

  // Sticky expand: once a project has an active session, record expand=true so
  // the user's inline session view doesn't silently collapse when activity ends.
  // Pin (for focus mode) is a separate, user-curated axis — see togglePinned.
  // `autoExpandedRef` guarantees each project is auto-expanded at most once, so
  // repeated sidebar recomputes during a turn don't re-walk/re-set the tree.
  const autoExpandedRef = useRef<Set<string>>(new Set());
  useEffect(() => {
    const handled = autoExpandedRef.current;
    const toExpand: string[] = [];
    const visit = (id: string, hasActive: boolean) => {
      if (!hasActive || handled.has(id)) return;
      handled.add(id);
      if (!(id in expandedProjects)) toExpand.push(id);
    };
    for (const f of orderedFolders) {
      for (const e of f.projects) visit(e.project.id, e.active.length > 0);
    }
    for (const e of ungrouped) visit(e.project.id, e.active.length > 0);
    if (toExpand.length > 0) setManyProjectsExpanded(toExpand, true);
  }, [orderedFolders, ungrouped, expandedProjects, setManyProjectsExpanded]);

  const anythingExpanded = useMemo(() => {
    if (allFolderNames.some((n) => expandedFolders[n] ?? true)) return true;
    return allProjectIds.some((id) => expandedProjects[id] === true);
  }, [allFolderNames, allProjectIds, expandedFolders, expandedProjects]);

  const toggleCollapseAll = useCallback(() => {
    const next = !anythingExpanded;
    setManyFoldersExpanded(allFolderNames, next);
    setManyProjectsExpanded(allProjectIds, next);
  }, [
    anythingExpanded,
    allFolderNames,
    allProjectIds,
    setManyFoldersExpanded,
    setManyProjectsExpanded,
  ]);

  return {
    isFolderExpanded,
    isProjectExpanded,
    toggleFolder,
    toggleProject,
    expandProject,
    anythingExpanded,
    toggleCollapseAll,
  };
}
