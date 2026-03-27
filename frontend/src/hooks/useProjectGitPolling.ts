import { useEffect } from "react";
import { useWebSocket } from "~/hooks/useWebSocket";
import { getProjectGitStatus } from "~/lib/project-actions";
import type { Project } from "~/lib/types";
import { useAppStore } from "~/stores/app-store";

const POLL_INTERVAL_MS = 10_000;

/**
 * Polls project-level git status for all projects on an interval.
 * Mount once at the app level alongside useGlobalSubscriptions.
 */
export function useProjectGitPolling(projects: Project[]) {
  const ws = useWebSocket();

  useEffect(() => {
    if (projects.length === 0) return;

    const poll = () => {
      for (const project of projects) {
        getProjectGitStatus(ws, project.id)
          .then((status) => useAppStore.getState().setProjectGitStatus(status))
          .catch(() => {});
      }
    };

    const timer = setInterval(poll, POLL_INTERVAL_MS);
    return () => clearInterval(timer);
  }, [ws, projects]);
}
