import { createFileRoute } from "@tanstack/react-router";
import { ProjectsPage } from "~/components/projects/ProjectsPage";

/**
 * Every checkout's state on disk. The registration list is Settings ›
 * Projects; see the header of `ProjectsPage` for why both exist.
 */
export const Route = createFileRoute("/projects")({
  component: ProjectsPage,
});
