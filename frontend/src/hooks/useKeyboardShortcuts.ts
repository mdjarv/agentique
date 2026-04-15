import { useNavigate } from "@tanstack/react-router";
import { useEffect } from "react";
export function useKeyboardShortcuts(projectSlug: string, projectId: string) {
  const navigate = useNavigate();

  useEffect(() => {
    if (!projectId) return;
    const handler = (e: KeyboardEvent) => {
      const mod = e.metaKey || e.ctrlKey;

      // Ctrl/Cmd+N: new chat
      if (mod && e.key === "n") {
        e.preventDefault();
        navigate({
          to: "/project/$projectSlug/session/new",
          params: { projectSlug },
        });
        return;
      }

      // Ctrl/Cmd+E: open file browser
      if (mod && e.key === "e") {
        e.preventDefault();
        navigate({
          to: "/project/$projectSlug/files",
          params: { projectSlug },
        });
        return;
      }
    };

    window.addEventListener("keydown", handler);
    return () => window.removeEventListener("keydown", handler);
  }, [projectSlug, projectId, navigate]);
}
