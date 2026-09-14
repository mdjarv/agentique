/**
 * `@file` completion for a project that is a plain folder, one directory at a
 * time — the way a shell completes a path.
 *
 * A git project completes from `git ls-files`, a flat list of everything worth
 * naming. A folder has no such list, and building one means walking a tree
 * that, for a home directory, holds caches, `node_modules` and the agentique
 * data dir itself. So the query is read as a path: everything up to the last
 * `/` names the directory to list, and what follows filters its entries. One
 * listing per directory, nothing recursive, and accepting a directory
 * continues completion inside it.
 */
import type { FileEntry } from "~/lib/api";

export interface DirQuery {
  /** Directory to list, relative to the project root: "" or ending in "/". */
  dir: string;
  /** What the entry names must start with. */
  prefix: string;
}

/** Splits a completion query at its last `/`. */
export function splitDirQuery(query: string): DirQuery {
  const cut = query.lastIndexOf("/");
  if (cut < 0) return { dir: "", prefix: query };
  return { dir: query.slice(0, cut + 1), prefix: query.slice(cut + 1) };
}

export interface DirCompletion {
  /** The path as inserted after `@`: a directory ends in `/`. */
  value: string;
  isDir: boolean;
}

/**
 * The entries of one directory listing that complete `prefix`, directories
 * first, capped at `limit`.
 *
 * Case-insensitive prefix match. Dotfiles are left out unless the prefix
 * itself starts with `.`, the shell's convention, which is what keeps a home
 * directory's completion to the folders somebody actually works in.
 */
export function dirCompletions(
  entries: readonly FileEntry[],
  { dir, prefix }: DirQuery,
  limit: number,
): DirCompletion[] {
  const p = prefix.toLowerCase();
  const showHidden = prefix.startsWith(".");
  const matches = entries.filter(
    (e) => (showHidden || !e.name.startsWith(".")) && e.name.toLowerCase().startsWith(p),
  );
  matches.sort((a, b) => {
    if (a.isDir !== b.isDir) return a.isDir ? -1 : 1;
    return a.name.localeCompare(b.name, undefined, { sensitivity: "base" });
  });
  return matches.slice(0, limit).map((e) => ({
    value: `${dir}${e.name}${e.isDir ? "/" : ""}`,
    isDir: e.isDir,
  }));
}
