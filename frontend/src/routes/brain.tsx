import { createFileRoute, redirect } from "@tanstack/react-router";

/**
 * The brain became the assistant's memory and the page moved under it. The old
 * path stays as a redirect: it is in bookmarks, and it is the URL every deep
 * link agentique has minted for this page.
 */
export const Route = createFileRoute("/brain")({
  beforeLoad: () => {
    throw redirect({ to: "/assistant/memory" });
  },
});
