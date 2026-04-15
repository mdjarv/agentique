import type { NavigateFn } from "@tanstack/react-router";
import { sessionShortId } from "~/lib/utils";

export function navigateToSession(navigate: NavigateFn, projectSlug: string, sessionId: string) {
  navigate({
    to: "/project/$projectSlug/session/$sessionShortId",
    params: { projectSlug, sessionShortId: sessionShortId(sessionId) },
  });
}
