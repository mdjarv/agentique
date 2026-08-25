import type { useNavigate } from "@tanstack/react-router";
import { useUIStore } from "~/stores/ui-store";

/** Prefix that separates new-session drafts from per-session composer drafts,
 *  which are keyed by the session id itself. */
const NEW_SESSION_DRAFT_PREFIX = "new:";

/** Draft key for a project's not-yet-created "new session" composer.
 *  `NewChatPanel` reads this as the composer's initial text and only creates the
 *  session when the user sends — so writing it is orphan-free. */
export function newSessionDraftKey(projectId: string): string {
  return `${NEW_SESSION_DRAFT_PREFIX}${projectId}`;
}

/**
 * The inverse: the project a draft key belongs to, or null when the key is a
 * per-session draft. The sidebar's Drafts section walks the whole draft map, so
 * the format is parsed here rather than re-spelled at the call site.
 */
export function newSessionDraftProjectId(key: string): string | null {
  if (!key.startsWith(NEW_SESSION_DRAFT_PREFIX)) return null;
  const projectId = key.slice(NEW_SESSION_DRAFT_PREFIX.length);
  return projectId || null;
}

type NavigateFn = ReturnType<typeof useNavigate>;

/**
 * Open the new-session view for a project with `text` pre-filled into its composer,
 * WITHOUT creating a session or sending anything. `NewChatPanel` defers session
 * creation until the user actually sends, so nothing (session, worktree, branch) is
 * materialized until they commit — abandoning the draft costs nothing.
 *
 * Appends to any in-progress new-session draft rather than clobbering it, so a
 * half-written prompt for the target project is never lost.
 */
export function openPrefilledNewSession(
  navigate: NavigateFn,
  { projectId, projectSlug, text }: { projectId: string; projectSlug: string; text: string },
): void {
  const key = newSessionDraftKey(projectId);
  const existing = useUIStore.getState().drafts[key] ?? "";
  const next = existing.trim() ? `${existing.trimEnd()}\n\n${text}` : text;
  useUIStore.getState().setDraft(key, next);
  navigate({ to: "/project/$projectSlug/session/new", params: { projectSlug } });
}
