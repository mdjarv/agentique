import { useEffect } from "react";
import { listProjects } from "~/lib/api";
import { useAppStore } from "~/stores/app-store";

export function useProjects() {
  const projects = useAppStore((s) => s.projects);
  const setProjects = useAppStore((s) => s.setProjects);

  useEffect(() => {
    listProjects().then(setProjects).catch(console.error);
  }, [setProjects]);

  return projects;
}
