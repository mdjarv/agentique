import { createFileRoute } from "@tanstack/react-router";
import { TemplatesSettings } from "~/components/settings/TemplatesSettings";

export const Route = createFileRoute("/settings/templates")({
  component: TemplatesSettings,
});
