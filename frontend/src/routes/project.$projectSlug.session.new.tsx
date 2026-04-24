import { createFileRoute } from "@tanstack/react-router";
import { z } from "zod";
import { NewChatPanel } from "~/components/chat/NewChatPanel";
import { useAppStore } from "~/stores/app-store";

const searchSchema = z.object({
  prompt: z.string().optional(),
  worktree: z.boolean().optional(),
});

export const Route = createFileRoute("/project/$projectSlug/session/new")({
  component: NewChatPage,
  validateSearch: searchSchema,
});

function NewChatPage() {
  const { projectSlug } = Route.useParams();
  const { prompt, worktree } = Route.useSearch();
  const project = useAppStore((s) => s.projects.find((p) => p.slug === projectSlug));
  if (!project) return null;
  return (
    <NewChatPanel
      projectId={project.id}
      projectSlug={projectSlug}
      initialPrompt={prompt}
      initialWorktree={worktree}
    />
  );
}
