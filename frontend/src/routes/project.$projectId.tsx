import { Outlet, createFileRoute } from "@tanstack/react-router";
import { useKeyboardShortcuts } from "~/hooks/useKeyboardShortcuts";
import { useProjectSubscription } from "~/hooks/useProjectSubscription";

export const Route = createFileRoute("/project/$projectId")({
  component: ProjectLayout,
});

function ProjectLayout() {
  const { projectId } = Route.useParams();
  useProjectSubscription(projectId);
  useKeyboardShortcuts(projectId);
  return <Outlet />;
}
