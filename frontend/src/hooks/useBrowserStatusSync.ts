import { useEffect } from "react";
import { useWebSocket } from "~/hooks/useWebSocket";
import { useBrowserStore } from "~/stores/browser-store";
import { useUIStore } from "~/stores/ui-store";

interface BrowserStatusResponse {
  sessions: Record<string, { running: boolean; url: string }>;
}

/**
 * Syncs browser state from the backend when the active session changes.
 * - If the session has a running browser, marks it as launched and opens the
 *   dock on Browser.
 * - Runs on mount and on session switch to survive page reloads.
 */
export function useBrowserStatusSync(sessionId: string | null): void {
  const ws = useWebSocket();

  useEffect(() => {
    if (!sessionId) return;

    const session = useBrowserStore.getState().sessions[sessionId];
    if (session?.launched || session?.launching) return;

    let stale = false;

    ws.request<BrowserStatusResponse>("browser.status", { sessionIds: [sessionId] })
      .then((resp) => {
        if (stale) return;
        const info = resp.sessions?.[sessionId];
        if (!info?.running) return;
        useBrowserStore.getState().setLaunched(sessionId);
        if (info.url) {
          useBrowserStore.getState().setUrl(sessionId, info.url);
        }
        // A browser this session already launched is worth showing, but not
        // worth stealing a dock the user has open on something else.
        if (!useUIStore.getState().dock[sessionId]?.open) {
          useUIStore.getState().openDock(sessionId, "browser");
        }
      })
      .catch(() => {
        // Status check failed — not critical
      });

    return () => {
      stale = true;
    };
  }, [ws, sessionId]);
}
