import { useEffect } from "react";
import { type SessionState, useChatStore } from "~/stores/chat-store";

const statePriority: Record<SessionState, number> = {
  running: 0,
  starting: 1,
  idle: 2,
  draft: 3,
  disconnected: 4,
  failed: 5,
  stopped: 6,
  done: 7,
};

export function useKeyboardShortcuts(projectId: string) {
  useEffect(() => {
    const handler = (e: KeyboardEvent) => {
      const mod = e.metaKey || e.ctrlKey;

      // Ctrl/Cmd+N: new draft session
      if (mod && e.key === "n") {
        e.preventDefault();
        useChatStore.getState().createDraft(projectId);
        return;
      }

      // Ctrl/Cmd+1-9: switch session by index
      if (mod && e.key >= "1" && e.key <= "9") {
        e.preventDefault();
        const store = useChatStore.getState();
        const sorted = Object.keys(store.sessions).sort((a, b) => {
          const sa = store.sessions[a]?.meta;
          const sb = store.sessions[b]?.meta;
          if (!sa || !sb) return 0;
          const pa = statePriority[sa.state] ?? 99;
          const pb = statePriority[sb.state] ?? 99;
          if (pa !== pb) return pa - pb;
          return new Date(sb.createdAt).getTime() - new Date(sa.createdAt).getTime();
        });
        const idx = Number.parseInt(e.key, 10) - 1;
        const targetId = sorted[idx];
        if (targetId) {
          store.setActiveSessionId(targetId);
        }
      }
    };

    window.addEventListener("keydown", handler);
    return () => window.removeEventListener("keydown", handler);
  }, [projectId]);
}
