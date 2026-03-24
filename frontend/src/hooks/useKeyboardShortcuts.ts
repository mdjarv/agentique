import { useNavigate } from "@tanstack/react-router";
import { useEffect } from "react";
import { type SessionState, useChatStore } from "~/stores/chat-store";

const statePriority: Record<SessionState, number> = {
  running: 0,
  merging: 1,
  idle: 2,
  failed: 3,
  stopped: 4,
  done: 5,
};

export function useKeyboardShortcuts(projectId: string) {
  const navigate = useNavigate();

  useEffect(() => {
    const handler = (e: KeyboardEvent) => {
      const mod = e.metaKey || e.ctrlKey;

      // Ctrl/Cmd+N: new chat
      if (mod && e.key === "n") {
        e.preventDefault();
        navigate({
          to: "/project/$projectId/session/new",
          params: { projectId },
        });
        return;
      }

      // Ctrl/Cmd+1-9: switch session by index
      if (mod && e.key >= "1" && e.key <= "9") {
        e.preventDefault();
        const store = useChatStore.getState();
        const sorted = Object.keys(store.sessions)
          .filter((id) => store.sessions[id]?.meta.projectId === projectId)
          .sort((a, b) => {
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
          navigate({
            to: "/project/$projectId/session/$sessionId",
            params: { projectId, sessionId: targetId },
          });
        }
      }
    };

    window.addEventListener("keydown", handler);
    return () => window.removeEventListener("keydown", handler);
  }, [projectId, navigate]);
}
