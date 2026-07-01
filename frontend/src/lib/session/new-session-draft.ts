import type { useNavigate } from "@tanstack/react-router";
import { useUIStore } from "~/stores/ui-store";

/** Draft key for a project's not-yet-created "new session" composer.
 *  `NewChatPanel` reads this as the composer's initial text and only creates the
 *  session when the user sends — so writing it is orphan-free. */
export function newSessionDraftKey(projectId: string): string {
  return `new:${projectId}`;
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
