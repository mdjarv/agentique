/**
 * Drives the two git sweeps (see `lib/git/sync-sweep.ts`) for the whole app.
 * Mount once, alongside the other global subscriptions.
 *
 * Succeeds `useProjectGitPolling`, which asked every project for status every
 * ten seconds. Status now runs on a minute, fetch on ten minutes, and both are
 * staggered and abortable so a re-render (or a project list that changed
 * mid-sweep) can't leave two sweeps racing each other.
 */
import { useEffect } from "react";
import { useWebSocket } from "~/hooks/useWebSocket";
import {
  FETCH_INTERVAL_MS,
  fetchSweep,
  STATUS_INTERVAL_MS,
  statusSweep,
} from "~/lib/git/sync-sweep";
import type { Project } from "~/lib/types";

/** Let the app finish booting before the first remote round-trips. */
const FIRST_FETCH_DELAY_MS = 8_000;

export function useSyncSweep(projects: Project[]): void {
  const ws = useWebSocket();

  useEffect(() => {
    if (projects.length === 0) return;
    const controller = new AbortController();
    const { signal } = controller;

    void statusSweep(ws, projects, signal);
    const statusTimer = setInterval(
      () => void statusSweep(ws, projects, signal),
      STATUS_INTERVAL_MS,
    );

    const firstFetch = setTimeout(
      () => void fetchSweep(ws, projects, signal),
      FIRST_FETCH_DELAY_MS,
    );
    const fetchTimer = setInterval(() => void fetchSweep(ws, projects, signal), FETCH_INTERVAL_MS);

    return () => {
      controller.abort();
      clearInterval(statusTimer);
      clearInterval(fetchTimer);
      clearTimeout(firstFetch);
    };
  }, [ws, projects]);
}
