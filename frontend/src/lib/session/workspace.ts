/**
 * Where a session's edits land: its own worktree, or the project's checkout.
 *
 * This is the one fact the row cannot infer from anything else it shows, and it
 * changes what a stray edit costs — a worktree session is on its own branch and
 * throwaway, a local one is typing straight into the working copy you have open
 * in an editor. So it gets a mark.
 *
 * The pairing lives here rather than at either render site, for the reason
 * `REST_GLYPH` does: the sidebar row and the session header both show it, and a
 * session that reads as a worktree in the rail cannot read as local in the pane
 * it opens. The kind union is closed, so a third arrangement has to choose its
 * own mark rather than inherit a blank.
 */
import { FolderOpen, GitBranch, type LucideIcon } from "lucide-react";

export type WorkspaceKind = "worktree" | "local";

/**
 * A session is a worktree session iff it has a branch of its own. Absent means
 * local — the session runs in the project's own checkout — which is why this
 * reads the branch rather than the path: `worktreePath` is set for both.
 */
export function workspaceKind(worktreeBranch?: string | null): WorkspaceKind {
  return worktreeBranch ? "worktree" : "local";
}

export const WORKSPACE_GLYPH: Record<WorkspaceKind, LucideIcon> = {
  worktree: GitBranch,
  // An open folder rather than FolderGit2: at 10px the branch node inside the
  // folder collapses into noise, and the two glyphs then differ by a detail
  // too small to see. Open-vs-branch is the distinction, so draw that.
  local: FolderOpen,
};

/** The word each kind goes by in UI copy. */
export const WORKSPACE_LABEL: Record<WorkspaceKind, string> = {
  worktree: "worktree",
  local: "local",
};

/**
 * What the mark says on hover, and to a screen reader. The worktree case names
 * the branch when there is one to name, because "which branch" is the next
 * question a worktree mark raises.
 */
export function workspaceTitle(kind: WorkspaceKind, branch?: string | null): string {
  if (kind === "local") return "Runs in the project's own checkout — no worktree isolation";
  return branch ? `Worktree on ${branch}` : "Runs in its own worktree";
}
