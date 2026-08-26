import { createFileRoute } from "@tanstack/react-router";
import { VoiceSettings } from "~/components/settings/VoiceSettings";

export const Route = createFileRoute("/settings/voice")({
  component: VoiceSettings,
});
