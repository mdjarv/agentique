import type { LucideIcon } from "lucide-react";
import { useEffect, useState } from "react";
import { getProjectIcon, preloadProjectIcon } from "~/lib/project-icons";

/**
 * Reactively resolve a project icon by ID.
 * Returns the icon immediately if cached (static or already preloaded),
 * otherwise triggers an async preload and re-renders when ready.
 */
export function useProjectIcon(iconId: string): LucideIcon | undefined {
  const [icon, setIcon] = useState(() => getProjectIcon(iconId));

  useEffect(() => {
    if (!iconId) return;
    // Check cache first (may have been loaded by another instance)
    const cached = getProjectIcon(iconId);
    if (cached) {
      setIcon(cached);
      return;
    }
    let cancelled = false;
    preloadProjectIcon(iconId).then(() => {
      if (cancelled) return;
      const loaded = getProjectIcon(iconId);
      if (loaded) setIcon(loaded);
    });
    return () => {
      cancelled = true;
    };
  }, [iconId]);

  return icon;
}
