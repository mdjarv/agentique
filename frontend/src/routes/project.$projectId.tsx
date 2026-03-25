import { Outlet, createFileRoute, useRouterState } from "@tanstack/react-router";
import { useKeyboardShortcuts } from "~/hooks/useKeyboardShortcuts";

export const Route = createFileRoute("/project/$projectId")({
  component: ProjectLayout,
});

function ProjectLayout() {
  const { projectId } = Route.useParams();
  const matchKey = useRouterState({ select: (s) => s.location.pathname });
  useKeyboardShortcuts(projectId);
  return <Outlet key={matchKey} />;
}
