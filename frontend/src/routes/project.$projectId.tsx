import { Outlet, createFileRoute } from "@tanstack/react-router";
import { useKeyboardShortcuts } from "~/hooks/useKeyboardShortcuts";

export const Route = createFileRoute("/project/$projectId")({
  component: ProjectLayout,
});

function ProjectLayout() {
  const { projectId } = Route.useParams();
  useKeyboardShortcuts(projectId);
  return <Outlet />;
}
