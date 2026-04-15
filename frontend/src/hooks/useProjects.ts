import { useEffect } from "react";
import { listProjects } from "~/lib/api";
import { getProjectIcon, preloadProjectIcon } from "~/lib/project-icons";
import { useAppStore } from "~/stores/app-store";

export function useProjects() {
  const projects = useAppStore((s) => s.projects);
  const setProjects = useAppStore((s) => s.setProjects);

  useEffect(() => {
    listProjects()
      .then((ps) => {
        setProjects(ps);
        for (const p of ps) {
          if (p.icon && !getProjectIcon(p.icon)) {
            preloadProjectIcon(p.icon);
          }
        }
      })
      .catch(console.error);
  }, [setProjects]);

  return projects;
}
