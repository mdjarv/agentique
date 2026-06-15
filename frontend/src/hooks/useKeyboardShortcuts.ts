import { useNavigate } from "@tanstack/react-router";
import { useEffect } from "react";
export function useKeyboardShortcuts(projectSlug: string, projectId: string) {
  const navigate = useNavigate();

  useEffect(() => {
    if (!projectId) return;
    const handler = (e: KeyboardEvent) => {
      const mod = e.metaKey || e.ctrlKey;

      // Ignore while typing in an editable element so Cmd+N etc. don't hijack
      // text entry; lowercase the key so Shift/CapsLock ("N") still matches.
      const target = e.target as HTMLElement | null;
      if (
        target &&
        (target.isContentEditable ||
          target.tagName === "INPUT" ||
          target.tagName === "TEXTAREA" ||
          target.tagName === "SELECT")
      ) {
        return;
      }
      const key = e.key.toLowerCase();

      // Ctrl/Cmd+N: new chat
      if (mod && key === "n") {
        e.preventDefault();
        navigate({
          to: "/project/$projectSlug/session/new",
          params: { projectSlug },
        });
        return;
      }

      // Ctrl/Cmd+E: open file browser
      if (mod && key === "e") {
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
