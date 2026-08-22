import { createFileRoute, redirect } from "@tanstack/react-router";

/** Bare /settings lands on the first category rather than an empty panel. */
export const Route = createFileRoute("/settings/")({
  beforeLoad: () => {
    throw redirect({ to: "/settings/machines" });
  },
});
