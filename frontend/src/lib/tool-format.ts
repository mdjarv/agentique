/**
 * Strips a project or worktree path prefix from an absolute path, yielding a
 * repo-relative display path. Worktree is checked first (it is the more specific
 * prefix). Shared by the inline tool block and the approval banner so the two
 * never drift on how a tool's file argument is rendered.
 */
export function stripPrefix(path: string, projectPath?: string, worktreePath?: string): string {
  for (const prefix of [worktreePath, projectPath]) {
    if (prefix && path.startsWith(prefix)) {
      const stripped = path.slice(prefix.length);
      return stripped.startsWith("/") ? stripped.slice(1) : stripped;
    }
  }
  return path;
}
