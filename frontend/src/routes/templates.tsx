import { createFileRoute, redirect } from "@tanstack/react-router";

/** The prompt library moved into Settings; the old path still resolves. */
export const Route = createFileRoute("/templates")({
  beforeLoad: () => {
    throw redirect({ to: "/settings/templates" });
  },
});
