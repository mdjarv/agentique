import { createFileRoute } from "@tanstack/react-router";
import { AssistantPage } from "~/components/assistant/AssistantPage";

export const Route = createFileRoute("/assistant")({
  component: AssistantPage,
});
