import { createFileRoute } from "@tanstack/react-router";
import { SessionsSettings } from "~/components/settings/SessionsSettings";

export const Route = createFileRoute("/settings/sessions")({
  component: SessionsSettings,
});
