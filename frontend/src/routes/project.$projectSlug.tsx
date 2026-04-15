import { createFileRoute, Outlet, useRouterState } from "@tanstack/react-router";
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
  useKeyboardShortcuts(projectSlug, project?.id ?? "");

  // Key by route pattern (routeId), not pathname. Same route pattern with different
  // params (e.g. switching sessions) reuses the component — avoids expensive
  // unmount/remount of ChatPanel. Different route types (session → files) still remount.
  const routeId = useRouterState({
    select: (s) => s.matches[s.matches.length - 1]?.routeId,
  });

  if (!projectsLoaded) {
    return <StatusPage message="Loading..." />;
  }

  if (!project) {
    return <StatusPage message="Project not found" />;
  }

  return <Outlet key={routeId} />;
}
