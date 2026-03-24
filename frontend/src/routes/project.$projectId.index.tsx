import { createFileRoute } from "@tanstack/react-router";

export const Route = createFileRoute("/project/$projectId/")({
  component: ProjectIndex,
});

function ProjectIndex() {
  return (
    <div className="flex flex-col h-full items-center justify-center text-muted-foreground">
      <p className="text-sm">Select a session or start a new chat</p>
    </div>
  );
}
