/**
 * Whether a project is git-based, the one place the client decides it.
 *
 * The server derives `kind` from the project's own folder on every read: `git`
 * when that exact folder is a repository root, `folder` otherwise — including a
 * folder that sits inside some other repository. A folder project's sessions
 * run straight in it, so there is no worktree to offer and no branch, diff or
 * merge surface to draw.
 *
 * `kind` is optional on the wire. A peer on a release from before it sends none,
 * and absence means "not reported": that peer's projects keep the git features
 * they have always had. Only an explicit `folder` turns them off.
 */
import type { Project } from "~/lib/types";
import { useAppStore } from "~/stores/app-store";

export function isFolderProject(project: Pick<Project, "kind"> | null | undefined): boolean {
  return project?.kind === "folder";
}

/** Non-reactive lookup by id, for event handlers and actions. */
export function isFolderProjectId(projectId: string | undefined): boolean {
  if (!projectId) return false;
  return isFolderProject(useAppStore.getState().projects.find((p) => p.id === projectId));
}

/** Reactive lookup by id. Returns a primitive, so the selector is stable. */
export function useIsFolderProject(projectId: string | undefined): boolean {
  return useAppStore((s) =>
    projectId ? isFolderProject(s.projects.find((p) => p.id === projectId)) : false,
  );
}
