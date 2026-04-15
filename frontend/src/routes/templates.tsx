import { createFileRoute } from "@tanstack/react-router";
import { TemplateListPage } from "~/components/templates/TemplateListPage";

export const Route = createFileRoute("/templates")({
  component: TemplateListPage,
});
