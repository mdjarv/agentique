import { type NavigateFn, useRouter } from "@tanstack/react-router";
import { useCallback } from "react";
import { sessionShortId } from "~/lib/utils";

/**
 * Guard for a `navigate()` that fires *after* an await.
 *
 * Sending a message or creating a session is a round trip; the user is free to
 * pick another session while it is in flight. A navigate resolved from that
 * stale closure yanks them back to where they started typing — so snapshot the
 * location before the await and only navigate if they have not moved.
 *
 *   const guard = useNavigationGuard();
 *   const stillHere = guard();
 *   await send();
 *   if (stillHere()) navigate({ ... });
 */
export function useNavigationGuard(): () => () => boolean {
  const router = useRouter();
  return useCallback(() => {
    const from = router.state.location.pathname;
    return () => router.state.location.pathname === from;
  }, [router]);
}

export function navigateToSession(navigate: NavigateFn, projectSlug: string, sessionId: string) {
  navigate({
    to: "/project/$projectSlug/session/$sessionShortId",
    params: { projectSlug, sessionShortId: sessionShortId(sessionId) },
  });
}
