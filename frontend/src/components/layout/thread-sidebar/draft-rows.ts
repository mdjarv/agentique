/**
 * Pure derivation for the sidebar's Drafts section — no React, no stores.
 *
 * A draft is a prompt typed into a project's New-session composer and never
 * sent. Nothing is materialized for it (no session, no worktree, no branch), so
 * it is not a session row: it has no state, no outcome and no pin, and the only
 * things it can say are which project it targets and what it says.
 */
import type { DraftRowVM } from "./types";

/** Longest excerpt kept in the VM. The row truncates visually anyway; this
 *  keeps a pasted spec out of the render tree. */
const TITLE_MAX = 160;

/**
 * The draft's own title: its first line with content, whitespace collapsed.
 * Empty when the draft is blank — such a row is dropped rather than rendered as
 * an anonymous placeholder the user cannot recognise.
 */
export function draftTitle(text: string): string {
  for (const line of text.split("\n")) {
    const trimmed = line.trim().replace(/\s+/g, " ");
    if (trimmed) return trimmed.slice(0, TITLE_MAX);
  }
  return "";
}

/** Whether anything follows the title line — the row is then an excerpt. */
export function draftHasMore(text: string): boolean {
  const lines = text.split("\n");
  const first = lines.findIndex((line) => line.trim());
  if (first < 0) return false;
  if ((lines[first] ?? "").trim().replace(/\s+/g, " ").length > TITLE_MAX) return true;
  return lines.slice(first + 1).some((line) => line.trim().length > 0);
}

/** Search match: the draft's own words, or the project it targets. */
export function draftMatchesQuery(vm: DraftRowVM, query: string): boolean {
  if (!query) return true;
  const q = query.toLowerCase();
  return (
    vm.title.toLowerCase().includes(q) ||
    vm.projectLabel.toLowerCase().includes(q) ||
    vm.projectSlug.toLowerCase().includes(q) ||
    vm.projectName.toLowerCase().includes(q)
  );
}

/**
 * Drafts carry no timestamp — the store holds text and nothing else — so there
 * is no recency to sort by and inventing one would reorder rows on a rehydrate.
 * Project name, then key: stable across renders and across reloads.
 */
export function compareDraftRows(a: DraftRowVM, b: DraftRowVM): number {
  return a.projectLabel.localeCompare(b.projectLabel) || a.draftKey.localeCompare(b.draftKey);
}
