import { Outlet, createFileRoute, useRouterState } from "@tanstack/react-router";
import { useKeyboardShortcuts } from "~/hooks/useKeyboardShortcuts";
import { useAppStore } from "~/stores/app-store";

export const Route = createFileRoute("/project/$projectSlug")({
  component: ProjectLayout,
});

function ProjectLayout() {
  const { projectSlug } = Route.useParams();
  const projectId = useAppStore((s) => s.projects.find((p) => p.slug === projectSlug)?.id ?? "");
  const matchKey = useRouterState({ select: (s) => s.location.pathname });
  useKeyboardShortcuts(projectSlug, projectId);
  return <Outlet key={matchKey} />;
}
