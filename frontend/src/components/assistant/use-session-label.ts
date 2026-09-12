import { sessionShortId } from "~/lib/utils";
import { useChatStore } from "~/stores/chat-store";

/**
 * What to call the session a journal entry or a proposal is about.
 *
 * The name comes from the session list this client already holds, which is the
 * live one: a session renamed since the row was written reads as what it is
 * called now. A session on a machine that is asleep, or one deleted since,
 * falls back to whatever name the row carried, and then to its short id — which
 * is what the rest of the app calls a session it cannot name.
 *
 * Shared by the recent-updates strip and the proposal card, because a session
 * the strip calls one thing cannot be a different name on the card above it.
 * Returns undefined for an entry about no session at all.
 */
export function useSessionLabel(
  sessionId: string | undefined,
  fallback?: string,
): string | undefined {
  const name = useChatStore((s) => (sessionId ? s.sessions[sessionId]?.meta.name : undefined));
  if (!sessionId) return undefined;
  return name || fallback || sessionShortId(sessionId);
}
