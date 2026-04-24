import { useEffect, useRef } from "react";
import { useWebSocket } from "~/hooks/useWebSocket";
import { fetchProject } from "~/lib/project-actions";
import { useAppStore } from "~/stores/app-store";
import { useChatStore } from "~/stores/chat-store";

const SWITCH_DEBOUNCE_MS = 2000;
const MIN_REFETCH_INTERVAL_MS = 30_000;
const IDLE_INTERVAL_MS = 5 * 60_000;
const HIDDEN_GRACE_MS = 5_000;

/**
 * Runs `git fetch` for the project containing the active session:
 *   - on project switch (debounced)
 *   - on tab becoming visible after a brief hidden period
 *   - on a slow interval while visible
 *
 * Each branch is rate-limited by MIN_REFETCH_INTERVAL_MS to avoid stacking.
 */
export function useActiveProjectFetch() {
  const ws = useWebSocket();
  const activeProjectId = useChatStore((s) => {
    const id = s.activeSessionId;
    if (!id) return null;
    return s.sessions[id]?.meta.projectId ?? null;
  });

  const lastFetchedRef = useRef<Map<string, number>>(new Map());

  useEffect(() => {
    if (!activeProjectId) return;

    const run = () => {
      const last = lastFetchedRef.current.get(activeProjectId) ?? 0;
      if (Date.now() - last < MIN_REFETCH_INTERVAL_MS) return;
      lastFetchedRef.current.set(activeProjectId, Date.now());
      fetchProject(ws, activeProjectId)
        .then((status) => useAppStore.getState().setProjectGitStatus(status))
        .catch((err) => console.error("active project fetch failed", err));
    };

    const switchTimer = setTimeout(run, SWITCH_DEBOUNCE_MS);

    const interval = setInterval(() => {
      if (document.visibilityState === "visible") run();
    }, IDLE_INTERVAL_MS);

    let hiddenAt = 0;
    const onVisibility = () => {
      if (document.visibilityState === "hidden") {
        hiddenAt = Date.now();
        return;
      }
      if (Date.now() - hiddenAt > HIDDEN_GRACE_MS) run();
    };
    document.addEventListener("visibilitychange", onVisibility);

    return () => {
      clearTimeout(switchTimer);
      clearInterval(interval);
      document.removeEventListener("visibilitychange", onVisibility);
    };
  }, [ws, activeProjectId]);
}
