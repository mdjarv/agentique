import { Outlet, createFileRoute, useRouterState } from "@tanstack/react-router";
import { StatusPage } from "~/components/layout/PageHeader";
import { useKeyboardShortcuts } from "~/hooks/useKeyboardShortcuts";
import { useAppStore } from "~/stores/app-store";

export const Route = createFileRoute("/project/$projectSlug")({
  component: ProjectLayout,
});

function ProjectLayout() {
  const { projectSlug } = Route.useParams();
  const projectsLoaded = useAppStore((s) => s.projectsLoaded);
  const project = useAppStore((s) => s.projects.find((p) => p.slug === projectSlug));
  const matchKey = useRouterState({ select: (s) => s.location.pathname });
  useKeyboardShortcuts(projectSlug, project?.id ?? "");

  if (!projectsLoaded) {
    return <StatusPage message="Loading..." />;
  }

  if (!project) {
    return <StatusPage message="Project not found" />;
  }

  return <Outlet key={matchKey} />;
}
