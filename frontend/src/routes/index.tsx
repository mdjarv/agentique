import { createFileRoute } from "@tanstack/react-router";

export const Route = createFileRoute("/")({
  component: HomePage,
});

function HomePage() {
  return (
    <div className="flex-1 flex items-center justify-center">
      <p className="text-muted-foreground text-lg">Select a project or create one to get started</p>
    </div>
  );
}
