import { createFileRoute, redirect } from "@tanstack/react-router";

/**
 * The repo inventory moved into Settings, where a registration belongs. The
 * old path stays as a redirect: it is in bookmarks, and it is the URL every
 * deep link agentique has ever minted for the project list points at.
 */
export const Route = createFileRoute("/projects")({
  beforeLoad: () => {
    throw redirect({ to: "/settings/projects" });
  },
});
